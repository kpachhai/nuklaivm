// Copyright (C) 2023, AllianceBlock. All rights reserved.
// See the file LICENSE for licensing terms.

package integration_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	avago_version "github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/fatih/color"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"go.uber.org/zap"

	"github.com/ava-labs/hypersdk/chain"
	"github.com/ava-labs/hypersdk/codec"
	"github.com/ava-labs/hypersdk/consts"
	"github.com/ava-labs/hypersdk/crypto/ed25519"
	"github.com/ava-labs/hypersdk/pubsub"
	"github.com/ava-labs/hypersdk/rpc"
	hutils "github.com/ava-labs/hypersdk/utils"
	"github.com/ava-labs/hypersdk/vm"

	"github.com/kpachhai/nuklaivm/actions"
	"github.com/kpachhai/nuklaivm/auth"
	lconsts "github.com/kpachhai/nuklaivm/consts"
	"github.com/kpachhai/nuklaivm/controller"
	"github.com/kpachhai/nuklaivm/genesis"
	lrpc "github.com/kpachhai/nuklaivm/rpc"
	"github.com/kpachhai/nuklaivm/utils"
)

var (
	logFactory logging.Factory
	log        logging.Logger
)

func init() {
	logFactory = logging.NewFactory(logging.Config{
		DisplayLevel: logging.Debug,
	})
	l, err := logFactory.Make("main")
	if err != nil {
		panic(err)
	}
	log = l
}

func TestIntegration(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "nuklaivm integration test suites")
}

var (
	requestTimeout time.Duration
	vms            int
)

func init() {
	flag.DurationVar(
		&requestTimeout,
		"request-timeout",
		120*time.Second,
		"timeout for transaction issuance and confirmation",
	)
	flag.IntVar(
		&vms,
		"vms",
		3,
		"number of VMs to create",
	)
}

var (
	priv    ed25519.PrivateKey
	factory *auth.ED25519Factory
	rsender ed25519.PublicKey
	sender  string

	priv2    ed25519.PrivateKey
	factory2 *auth.ED25519Factory
	rsender2 ed25519.PublicKey
	sender2  string

	priv3    ed25519.PrivateKey
	factory3 *auth.ED25519Factory
	rsender3 ed25519.PublicKey
	sender3  string

	// when used with embedded VMs
	genesisBytes []byte
	instances    []instance
	blocks       []snowman.Block

	networkID uint32
	gen       *genesis.Genesis
)

type instance struct {
	chainID           ids.ID
	nodeID            ids.NodeID
	vm                *vm.VM
	toEngine          chan common.Message
	JSONRPCServer     *httptest.Server
	BaseJSONRPCServer *httptest.Server
	WebSocketServer   *httptest.Server
	cli               *rpc.JSONRPCClient // clients for embedded VMs
	lcli              *lrpc.JSONRPCClient
}

var _ = ginkgo.BeforeSuite(func() {
	log.Info("VMID", zap.Stringer("id", lconsts.ID))
	gomega.Ω(vms).Should(gomega.BeNumerically(">", 1))

	var err error
	priv, err = ed25519.GeneratePrivateKey()
	gomega.Ω(err).Should(gomega.BeNil())
	factory = auth.NewED25519Factory(priv)
	rsender = priv.PublicKey()
	sender = utils.Address(rsender)
	log.Debug(
		"generated key",
		zap.String("addr", sender),
		zap.String("pk", hex.EncodeToString(priv[:])),
	)

	priv2, err = ed25519.GeneratePrivateKey()
	gomega.Ω(err).Should(gomega.BeNil())
	factory2 = auth.NewED25519Factory(priv2)
	rsender2 = priv2.PublicKey()
	sender2 = utils.Address(rsender2)
	log.Debug(
		"generated key",
		zap.String("addr", sender2),
		zap.String("pk", hex.EncodeToString(priv2[:])),
	)

	priv3, err = ed25519.GeneratePrivateKey()
	gomega.Ω(err).Should(gomega.BeNil())
	factory3 = auth.NewED25519Factory(priv3)
	rsender3 = priv3.PublicKey()
	sender3 = utils.Address(rsender3)
	log.Debug(
		"generated key",
		zap.String("addr", sender3),
		zap.String("pk", hex.EncodeToString(priv3[:])),
	)

	// create embedded VMs
	instances = make([]instance, vms)

	gen = genesis.Default()
	gen.MinUnitPrice = chain.Dimensions{1, 1, 1, 1, 1}
	gen.MinBlockGap = 0
	gen.CustomAllocation = []*genesis.CustomAllocation{
		{
			Address: sender,
			Balance: 10_000_000,
		},
	}
	genesisBytes, err = json.Marshal(gen)
	gomega.Ω(err).Should(gomega.BeNil())

	networkID = uint32(1)
	subnetID := ids.GenerateTestID()
	chainID := ids.GenerateTestID()

	app := &appSender{}
	for i := range instances {
		nodeID := ids.GenerateTestNodeID()
		sk, err := bls.NewSecretKey()
		gomega.Ω(err).Should(gomega.BeNil())
		l, err := logFactory.Make(nodeID.String())
		gomega.Ω(err).Should(gomega.BeNil())
		dname, err := os.MkdirTemp("", fmt.Sprintf("%s-chainData", nodeID.String()))
		gomega.Ω(err).Should(gomega.BeNil())
		snowCtx := &snow.Context{
			NetworkID:      networkID,
			SubnetID:       subnetID,
			ChainID:        chainID,
			NodeID:         nodeID,
			Log:            l,
			ChainDataDir:   dname,
			Metrics:        metrics.NewOptionalGatherer(),
			PublicKey:      bls.PublicFromSecretKey(sk),
			WarpSigner:     warp.NewSigner(sk, networkID, chainID),
			ValidatorState: &validators.TestState{},
		}

		toEngine := make(chan common.Message, 1)
		db := manager.NewMemDB(avago_version.CurrentDatabase)

		v := controller.New()
		err = v.Initialize(
			context.TODO(),
			snowCtx,
			db,
			genesisBytes,
			nil,
			[]byte(
				`{"parallelism":3, "testMode":true, "logLevel":"debug"}`,
			),
			toEngine,
			nil,
			app,
		)
		gomega.Ω(err).Should(gomega.BeNil())

		var hd map[string]*common.HTTPHandler
		hd, err = v.CreateHandlers(context.TODO())
		gomega.Ω(err).Should(gomega.BeNil())

		jsonRPCServer := httptest.NewServer(hd[rpc.JSONRPCEndpoint].Handler)
		ljsonRPCServer := httptest.NewServer(hd[lrpc.JSONRPCEndpoint].Handler)
		webSocketServer := httptest.NewServer(hd[rpc.WebSocketEndpoint].Handler)
		instances[i] = instance{
			chainID:           snowCtx.ChainID,
			nodeID:            snowCtx.NodeID,
			vm:                v,
			toEngine:          toEngine,
			JSONRPCServer:     jsonRPCServer,
			BaseJSONRPCServer: ljsonRPCServer,
			WebSocketServer:   webSocketServer,
			cli:               rpc.NewJSONRPCClient(jsonRPCServer.URL),
			lcli:              lrpc.NewJSONRPCClient(ljsonRPCServer.URL, snowCtx.NetworkID, snowCtx.ChainID),
		}

		// Force sync ready (to mimic bootstrapping from genesis)
		v.ForceReady()
	}

	// Verify genesis allocations loaded correctly (do here otherwise test may
	// check during and it will be inaccurate)
	for _, inst := range instances {
		cli := inst.lcli
		g, err := cli.Genesis(context.Background())
		gomega.Ω(err).Should(gomega.BeNil())

		csupply := uint64(0)
		for _, alloc := range g.CustomAllocation {
			balance, err := cli.Balance(context.Background(), alloc.Address)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(balance).Should(gomega.Equal(alloc.Balance))
			csupply += alloc.Balance
		}
	}
	blocks = []snowman.Block{}

	app.instances = instances
	color.Blue("created %d VMs", vms)
})

var _ = ginkgo.AfterSuite(func() {
	for _, iv := range instances {
		iv.JSONRPCServer.Close()
		iv.BaseJSONRPCServer.Close()
		iv.WebSocketServer.Close()
		err := iv.vm.Shutdown(context.TODO())
		gomega.Ω(err).Should(gomega.BeNil())
	}
})

var _ = ginkgo.Describe("[Ping]", func() {
	ginkgo.It("can ping", func() {
		for _, inst := range instances {
			cli := inst.cli
			ok, err := cli.Ping(context.Background())
			gomega.Ω(ok).Should(gomega.BeTrue())
			gomega.Ω(err).Should(gomega.BeNil())
		}
	})
})

var _ = ginkgo.Describe("[Network]", func() {
	ginkgo.It("can get network", func() {
		for _, inst := range instances {
			cli := inst.cli
			networkID, subnetID, chainID, err := cli.Network(context.Background())
			gomega.Ω(networkID).Should(gomega.Equal(uint32(1)))
			gomega.Ω(subnetID).ShouldNot(gomega.Equal(ids.Empty))
			gomega.Ω(chainID).ShouldNot(gomega.Equal(ids.Empty))
			gomega.Ω(err).Should(gomega.BeNil())
		}
	})
})

var _ = ginkgo.Describe("[Tx Processing]", func() {
	ginkgo.It("get currently accepted block ID", func() {
		for _, inst := range instances {
			cli := inst.cli
			_, _, _, err := cli.Accepted(context.Background())
			gomega.Ω(err).Should(gomega.BeNil())
		}
	})

	var transferTxRoot *chain.Transaction
	ginkgo.It("Gossip TransferTx to a different node", func() {
		ginkgo.By("issue TransferTx", func() {
			parser, err := instances[0].lcli.Parser(context.Background())
			gomega.Ω(err).Should(gomega.BeNil())
			submit, transferTx, _, err := instances[0].cli.GenerateTransaction(
				context.Background(),
				parser,
				nil,
				&actions.Transfer{
					To:    rsender2,
					Value: 100_000, // must be more than StateLockup
				},
				factory,
			)
			transferTxRoot = transferTx
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(submit(context.Background())).Should(gomega.BeNil())
			gomega.Ω(instances[0].vm.Mempool().Len(context.Background())).Should(gomega.Equal(1))
		})

		ginkgo.By("skip duplicate", func() {
			_, err := instances[0].cli.SubmitTx(
				context.Background(),
				transferTxRoot.Bytes(),
			)
			gomega.Ω(err).To(gomega.Not(gomega.BeNil()))
		})

		ginkgo.By("send gossip from node 0 to 1", func() {
			err := instances[0].vm.Gossiper().Force(context.TODO())
			gomega.Ω(err).Should(gomega.BeNil())
		})

		ginkgo.By("skip invalid time", func() {
			tx := chain.NewTx(
				&chain.Base{
					ChainID:   instances[0].chainID,
					Timestamp: 0,
					MaxFee:    1000,
				},
				nil,
				&actions.Transfer{
					To:    rsender2,
					Value: 110,
				},
			)
			// Must do manual construction to avoid `tx.Sign` error (would fail with
			// 0 timestamp)
			msg, err := tx.Digest()
			gomega.Ω(err).To(gomega.BeNil())
			auth, err := factory.Sign(msg, tx.Action)
			gomega.Ω(err).To(gomega.BeNil())
			tx.Auth = auth
			p := codec.NewWriter(0, consts.MaxInt) // test codec growth
			gomega.Ω(tx.Marshal(p)).To(gomega.BeNil())
			gomega.Ω(p.Err()).To(gomega.BeNil())
			_, err = instances[0].cli.SubmitTx(
				context.Background(),
				p.Bytes(),
			)
			gomega.Ω(err).To(gomega.Not(gomega.BeNil()))
		})

		ginkgo.By("skip duplicate (after gossip, which shouldn't clear)", func() {
			_, err := instances[0].cli.SubmitTx(
				context.Background(),
				transferTxRoot.Bytes(),
			)
			gomega.Ω(err).To(gomega.Not(gomega.BeNil()))
		})

		ginkgo.By("receive gossip in the node 1, and signal block build", func() {
			gomega.Ω(instances[1].vm.Builder().Force(context.TODO())).To(gomega.BeNil())
			<-instances[1].toEngine
		})

		ginkgo.By("build block in the node 1", func() {
			ctx := context.TODO()
			blk, err := instances[1].vm.BuildBlock(ctx)
			gomega.Ω(err).To(gomega.BeNil())

			gomega.Ω(blk.Verify(ctx)).To(gomega.BeNil())
			gomega.Ω(blk.Status()).To(gomega.Equal(choices.Processing))

			err = instances[1].vm.SetPreference(ctx, blk.ID())
			gomega.Ω(err).To(gomega.BeNil())

			gomega.Ω(blk.Accept(ctx)).To(gomega.BeNil())
			gomega.Ω(blk.Status()).To(gomega.Equal(choices.Accepted))
			blocks = append(blocks, blk)

			lastAccepted, err := instances[1].vm.LastAccepted(ctx)
			gomega.Ω(err).To(gomega.BeNil())
			gomega.Ω(lastAccepted).To(gomega.Equal(blk.ID()))

			results := blk.(*chain.StatelessBlock).Results()
			gomega.Ω(results).Should(gomega.HaveLen(1))
			gomega.Ω(results[0].Success).Should(gomega.BeTrue())
			gomega.Ω(results[0].Output).Should(gomega.BeNil())

			// Unit explanation
			//
			// bandwidth: tx size
			// compute: 5 for signature, 1 for base, 1 for transfer
			// read: 2 keys reads, 1 had 0 chunks
			// create: 1 key created
			// modify: 1 cold key modified
			transferTxConsumed := chain.Dimensions{190, 7, 12, 25, 13}
			gomega.Ω(results[0].Consumed).Should(gomega.Equal(transferTxConsumed))

			// Fee explanation
			//
			// Multiply all unit consumption by 1 and sum
			gomega.Ω(results[0].Fee).Should(gomega.Equal(uint64(247)))
		})

		ginkgo.By("ensure balance is updated", func() {
			balance, err := instances[1].lcli.Balance(context.Background(), sender)
			gomega.Ω(err).To(gomega.BeNil())
			gomega.Ω(balance).To(gomega.Equal(uint64(9899753)))
			balance2, err := instances[1].lcli.Balance(context.Background(), sender2)
			gomega.Ω(err).To(gomega.BeNil())
			gomega.Ω(balance2).To(gomega.Equal(uint64(100000)))
		})
	})

	ginkgo.It("ensure multiple txs work ", func() {
		ginkgo.By("transfer funds again", func() {
			parser, err := instances[1].lcli.Parser(context.Background())
			gomega.Ω(err).Should(gomega.BeNil())
			submit, _, _, err := instances[1].cli.GenerateTransaction(
				context.Background(),
				parser,
				nil,
				&actions.Transfer{
					To:    rsender2,
					Value: 101,
				},
				factory,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(submit(context.Background())).Should(gomega.BeNil())
			accept := expectBlk(instances[1])
			results := accept(true)
			gomega.Ω(results).Should(gomega.HaveLen(1))
			gomega.Ω(results[0].Success).Should(gomega.BeTrue())

			// Unit explanation
			//
			// bandwidth: tx size
			// compute: 5 for signature, 1 for base, 1 for transfer
			// read: 2 keys reads, 1 chunk each
			// create: 0 key created
			// modify: 2 cold key modified
			transferTxConsumed := chain.Dimensions{190, 7, 14, 0, 26}
			gomega.Ω(results[0].Consumed).Should(gomega.Equal(transferTxConsumed))

			// Fee explanation
			//
			// Multiply all unit consumption by 1 and sum
			gomega.Ω(results[0].Fee).Should(gomega.Equal(uint64(237)))

			balance2, err := instances[1].lcli.Balance(context.Background(), sender2)
			gomega.Ω(err).To(gomega.BeNil())
			gomega.Ω(balance2).To(gomega.Equal(uint64(100101)))
		})

		ginkgo.By("transfer funds again (test storage keys)", func() {
			parser, err := instances[1].lcli.Parser(context.Background())
			gomega.Ω(err).Should(gomega.BeNil())

			submit, _, _, err := instances[1].cli.GenerateTransaction(
				context.Background(),
				parser,
				nil,
				&actions.Transfer{
					To:    rsender2,
					Value: 102,
				},
				factory,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(submit(context.Background())).Should(gomega.BeNil())
			submit, _, _, err = instances[1].cli.GenerateTransaction(
				context.Background(),
				parser,
				nil,
				&actions.Transfer{
					To:    rsender2,
					Value: 103,
				},
				factory,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(submit(context.Background())).Should(gomega.BeNil())
			submit, _, _, err = instances[1].cli.GenerateTransaction(
				context.Background(),
				parser,
				nil,
				&actions.Transfer{
					To:    rsender3,
					Value: 104,
				},
				factory,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(submit(context.Background())).Should(gomega.BeNil())
			submit, _, _, err = instances[1].cli.GenerateTransaction(
				context.Background(),
				parser,
				nil,
				&actions.Transfer{
					To:    rsender3,
					Value: 105,
				},
				factory,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(submit(context.Background())).Should(gomega.BeNil())

			// Ensure we can handle case where accepted block is not processed
			latestBlock := blocks[len(blocks)-1]
			latestBlock.(*chain.StatelessBlock).MarkUnprocessed()

			// Accept new block (should use accepted state)
			accept := expectBlk(instances[1])
			results := accept(true)

			// Check results
			gomega.Ω(results).Should(gomega.HaveLen(4))

			// Unit explanation
			//
			// bandwidth: tx size
			// compute: 5 for signature, 1 for base, 1 for transfer
			// read: 2 cold keys reads, 1 chunk each
			// create: 0 key created
			// modify: 2 cold key modified
			gomega.Ω(results[0].Success).Should(gomega.BeTrue())
			transferTxConsumed := chain.Dimensions{190, 7, 14, 0, 26}
			gomega.Ω(results[0].Consumed).Should(gomega.Equal(transferTxConsumed))
			// Fee explanation
			//
			// Multiply all unit consumption by 1 and sum
			gomega.Ω(results[0].Fee).Should(gomega.Equal(uint64(237)))

			// Unit explanation
			//
			// bandwidth: tx size
			// compute: 5 for signature, 1 for base, 1 for transfer
			// read: 2 warm keys reads, 1 chunk each
			// create: 0 key created
			// modify: 2 warm keys modified
			gomega.Ω(results[1].Success).Should(gomega.BeTrue())
			transferTxConsumed = chain.Dimensions{190, 7, 4, 0, 16}
			gomega.Ω(results[1].Consumed).Should(gomega.Equal(transferTxConsumed))
			// Fee explanation
			//
			// Multiply all unit consumption by 1 and sum
			gomega.Ω(results[1].Fee).Should(gomega.Equal(uint64(217)))

			// Unit explanation
			//
			// bandwidth: tx size
			// compute: 5 for signature, 1 for base, 1 for transfer
			// read: 1 cold keys read (0 chunk), 1 warm key read (1 chunk)
			// create: 1 key created (1 chunk)
			// modify: 1 warm key modified (1 chunk)
			gomega.Ω(results[2].Success).Should(gomega.BeTrue())
			transferTxConsumed = chain.Dimensions{190, 7, 7, 25, 8}
			gomega.Ω(results[2].Consumed).Should(gomega.Equal(transferTxConsumed))
			// Fee explanation
			//
			// Multiply all unit consumption by 1 and sum
			gomega.Ω(results[2].Fee).Should(gomega.Equal(uint64(237)))

			// Unit explanation
			//
			// bandwidth: tx size
			// compute: 5 for signature, 1 for base, 1 for transfer
			// read: 2 warm keys reads (1 chunk, 0 chunk) -> note, this is based on disk BEFORE block
			// create: 0 key created
			// modify: 2 warm keys modified (1 chunk)
			gomega.Ω(results[3].Success).Should(gomega.BeTrue())
			transferTxConsumed = chain.Dimensions{190, 7, 3, 0, 16}
			gomega.Ω(results[3].Consumed).Should(gomega.Equal(transferTxConsumed))
			// Fee explanation
			//
			// Multiply all unit consumption by 1 and sum
			gomega.Ω(results[3].Fee).Should(gomega.Equal(uint64(216)))

			// Check end balance
			balance2, err := instances[1].lcli.Balance(context.Background(), sender2)
			gomega.Ω(err).To(gomega.BeNil())
			gomega.Ω(balance2).To(gomega.Equal(uint64(100306)))
			balance3, err := instances[1].lcli.Balance(context.Background(), sender3)
			gomega.Ω(err).To(gomega.BeNil())
			gomega.Ω(balance3).To(gomega.Equal(uint64(209)))
		})
	})

	ginkgo.It("Test processing block handling", func() {
		var accept, accept2 func(bool) []*chain.Result

		ginkgo.By("create processing tip", func() {
			parser, err := instances[1].lcli.Parser(context.Background())
			gomega.Ω(err).Should(gomega.BeNil())
			submit, _, _, err := instances[1].cli.GenerateTransaction(
				context.Background(),
				parser,
				nil,
				&actions.Transfer{
					To:    rsender2,
					Value: 200,
				},
				factory,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(submit(context.Background())).Should(gomega.BeNil())
			accept = expectBlk(instances[1])

			submit, _, _, err = instances[1].cli.GenerateTransaction(
				context.Background(),
				parser,
				nil,
				&actions.Transfer{
					To:    rsender2,
					Value: 201,
				},
				factory,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(submit(context.Background())).Should(gomega.BeNil())
			accept2 = expectBlk(instances[1])
		})

		ginkgo.By("clear processing tip", func() {
			results := accept(true)
			gomega.Ω(results).Should(gomega.HaveLen(1))
			gomega.Ω(results[0].Success).Should(gomega.BeTrue())
			results = accept2(true)
			gomega.Ω(results).Should(gomega.HaveLen(1))
			gomega.Ω(results[0].Success).Should(gomega.BeTrue())
		})
	})

	ginkgo.It("ensure mempool works", func() {
		ginkgo.By("fail Gossip TransferTx to a stale node when missing previous blocks", func() {
			parser, err := instances[1].lcli.Parser(context.Background())
			gomega.Ω(err).Should(gomega.BeNil())
			submit, _, _, err := instances[1].cli.GenerateTransaction(
				context.Background(),
				parser,
				nil,
				&actions.Transfer{
					To:    rsender2,
					Value: 203,
				},
				factory,
			)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(submit(context.Background())).Should(gomega.BeNil())

			err = instances[1].vm.Gossiper().Force(context.TODO())
			gomega.Ω(err).Should(gomega.BeNil())

			// mempool in 0 should be 1 (old amount), since gossip/submit failed
			gomega.Ω(instances[0].vm.Mempool().Len(context.TODO())).Should(gomega.Equal(1))
		})
	})

	ginkgo.It("ensure unprocessed tip works", func() {
		ginkgo.By("import accepted blocks to instance 2", func() {
			ctx := context.TODO()

			gomega.Ω(blocks[0].Height()).Should(gomega.Equal(uint64(1)))

			n := instances[2]
			blk1, err := n.vm.ParseBlock(ctx, blocks[0].Bytes())
			gomega.Ω(err).Should(gomega.BeNil())
			err = blk1.Verify(ctx)
			gomega.Ω(err).Should(gomega.BeNil())

			// Parse tip
			blk2, err := n.vm.ParseBlock(ctx, blocks[1].Bytes())
			gomega.Ω(err).Should(gomega.BeNil())
			blk3, err := n.vm.ParseBlock(ctx, blocks[2].Bytes())
			gomega.Ω(err).Should(gomega.BeNil())

			// Verify tip
			err = blk2.Verify(ctx)
			gomega.Ω(err).Should(gomega.BeNil())
			err = blk3.Verify(ctx)
			gomega.Ω(err).Should(gomega.BeNil())

			// Accept tip
			err = blk1.Accept(ctx)
			gomega.Ω(err).Should(gomega.BeNil())
			err = blk2.Accept(ctx)
			gomega.Ω(err).Should(gomega.BeNil())
			err = blk3.Accept(ctx)
			gomega.Ω(err).Should(gomega.BeNil())

			// Parse another
			blk4, err := n.vm.ParseBlock(ctx, blocks[3].Bytes())
			gomega.Ω(err).Should(gomega.BeNil())
			err = blk4.Verify(ctx)
			gomega.Ω(err).Should(gomega.BeNil())
			err = blk4.Accept(ctx)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(n.vm.SetPreference(ctx, blk4.ID())).Should(gomega.BeNil())
		})
	})

	ginkgo.It("processes valid index transactions (w/block listening)", func() {
		// Clear previous txs on instance 0
		accept := expectBlk(instances[0])
		accept(false) // don't care about results

		// Subscribe to blocks
		cli, err := rpc.NewWebSocketClient(instances[0].WebSocketServer.URL, rpc.DefaultHandshakeTimeout, pubsub.MaxPendingMessages, pubsub.MaxReadMessageSize)
		gomega.Ω(err).Should(gomega.BeNil())
		gomega.Ω(cli.RegisterBlocks()).Should(gomega.BeNil())

		// Wait for message to be sent
		time.Sleep(2 * pubsub.MaxMessageWait)

		// Fetch balances
		balance, err := instances[0].lcli.Balance(context.TODO(), sender)
		gomega.Ω(err).Should(gomega.BeNil())

		// Send tx
		other, err := ed25519.GeneratePrivateKey()
		gomega.Ω(err).Should(gomega.BeNil())
		transfer := &actions.Transfer{
			To:    other.PublicKey(),
			Value: 1,
		}

		parser, err := instances[0].lcli.Parser(context.Background())
		gomega.Ω(err).Should(gomega.BeNil())
		submit, _, _, err := instances[0].cli.GenerateTransaction(
			context.Background(),
			parser,
			nil,
			transfer,
			factory,
		)
		gomega.Ω(err).Should(gomega.BeNil())
		gomega.Ω(submit(context.Background())).Should(gomega.BeNil())

		gomega.Ω(err).Should(gomega.BeNil())
		accept = expectBlk(instances[0])
		results := accept(false)
		gomega.Ω(results).Should(gomega.HaveLen(1))
		gomega.Ω(results[0].Success).Should(gomega.BeTrue())

		// Read item from connection
		blk, lresults, prices, err := cli.ListenBlock(context.TODO(), parser)
		gomega.Ω(err).Should(gomega.BeNil())
		gomega.Ω(len(blk.Txs)).Should(gomega.Equal(1))
		tx := blk.Txs[0].Action.(*actions.Transfer)
		gomega.Ω(tx.Value).To(gomega.Equal(uint64(1)))
		gomega.Ω(lresults).Should(gomega.Equal(results))
		gomega.Ω(prices).Should(gomega.Equal(chain.Dimensions{1, 1, 1, 1, 1}))

		// Check balance modifications are correct
		balancea, err := instances[0].lcli.Balance(context.TODO(), sender)
		gomega.Ω(err).Should(gomega.BeNil())
		gomega.Ω(balance).Should(gomega.Equal(balancea + lresults[0].Fee + 1))

		// Close connection when done
		gomega.Ω(cli.Close()).Should(gomega.BeNil())
	})

	ginkgo.It("processes valid index transactions (w/streaming verification)", func() {
		// Create streaming client
		cli, err := rpc.NewWebSocketClient(instances[0].WebSocketServer.URL, rpc.DefaultHandshakeTimeout, pubsub.MaxPendingMessages, pubsub.MaxReadMessageSize)
		gomega.Ω(err).Should(gomega.BeNil())

		// Create tx
		other, err := ed25519.GeneratePrivateKey()
		gomega.Ω(err).Should(gomega.BeNil())
		transfer := &actions.Transfer{
			To:    other.PublicKey(),
			Value: 1,
		}
		parser, err := instances[0].lcli.Parser(context.Background())
		gomega.Ω(err).Should(gomega.BeNil())
		_, tx, _, err := instances[0].cli.GenerateTransaction(
			context.Background(),
			parser,
			nil,
			transfer,
			factory,
		)
		gomega.Ω(err).Should(gomega.BeNil())

		// Submit tx and accept block
		gomega.Ω(cli.RegisterTx(tx)).Should(gomega.BeNil())

		// Wait for message to be sent
		time.Sleep(2 * pubsub.MaxMessageWait)

		for instances[0].vm.Mempool().Len(context.TODO()) == 0 {
			// We need to wait for mempool to be populated because issuance will
			// return as soon as bytes are on the channel.
			hutils.Outf("{{yellow}}waiting for mempool to return non-zero txs{{/}}\n")
			time.Sleep(500 * time.Millisecond)
		}
		gomega.Ω(err).Should(gomega.BeNil())
		accept := expectBlk(instances[0])
		results := accept(false)
		gomega.Ω(results).Should(gomega.HaveLen(1))
		gomega.Ω(results[0].Success).Should(gomega.BeTrue())

		// Read decision from connection
		txID, dErr, result, err := cli.ListenTx(context.TODO())
		gomega.Ω(err).Should(gomega.BeNil())
		gomega.Ω(txID).Should(gomega.Equal(tx.ID()))
		gomega.Ω(dErr).Should(gomega.BeNil())
		gomega.Ω(result.Success).Should(gomega.BeTrue())
		gomega.Ω(result).Should(gomega.Equal(results[0]))

		// Close connection when done
		gomega.Ω(cli.Close()).Should(gomega.BeNil())
	})
})

func expectBlk(i instance) func(bool) []*chain.Result {
	ctx := context.TODO()

	// manually signal ready
	gomega.Ω(i.vm.Builder().Force(ctx)).Should(gomega.BeNil())
	// manually ack ready sig as in engine
	<-i.toEngine

	blk, err := i.vm.BuildBlock(ctx)
	if err != nil {
		panic(err)
	}
	gomega.Ω(err).To(gomega.BeNil())
	gomega.Ω(blk).To(gomega.Not(gomega.BeNil()))

	gomega.Ω(blk.Verify(ctx)).To(gomega.BeNil())
	gomega.Ω(blk.Status()).To(gomega.Equal(choices.Processing))

	err = i.vm.SetPreference(ctx, blk.ID())
	gomega.Ω(err).To(gomega.BeNil())

	return func(add bool) []*chain.Result {
		gomega.Ω(blk.Accept(ctx)).To(gomega.BeNil())
		gomega.Ω(blk.Status()).To(gomega.Equal(choices.Accepted))

		if add {
			blocks = append(blocks, blk)
		}

		lastAccepted, err := i.vm.LastAccepted(ctx)
		gomega.Ω(err).To(gomega.BeNil())
		gomega.Ω(lastAccepted).To(gomega.Equal(blk.ID()))
		return blk.(*chain.StatelessBlock).Results()
	}
}

var _ common.AppSender = &appSender{}

type appSender struct {
	next      int
	instances []instance
}

func (app *appSender) SendAppGossip(ctx context.Context, appGossipBytes []byte) error {
	n := len(app.instances)
	sender := app.instances[app.next].nodeID
	app.next++
	app.next %= n
	return app.instances[app.next].vm.AppGossip(ctx, sender, appGossipBytes)
}

func (*appSender) SendAppRequest(context.Context, set.Set[ids.NodeID], uint32, []byte) error {
	return nil
}

func (*appSender) SendAppResponse(context.Context, ids.NodeID, uint32, []byte) error {
	return nil
}

func (*appSender) SendAppGossipSpecific(context.Context, set.Set[ids.NodeID], []byte) error {
	return nil
}

func (*appSender) SendCrossChainAppRequest(context.Context, ids.ID, uint32, []byte) error {
	return nil
}

func (*appSender) SendCrossChainAppResponse(context.Context, ids.ID, uint32, []byte) error {
	return nil
}
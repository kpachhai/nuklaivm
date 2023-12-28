# nuklaivm

## Status

`nuklaivm` is considered **ALPHA** software and is not safe to use in
production. The framework is under active development and may change
significantly over the coming months as its modules are optimized and
audited.

## Demo

### Launch Subnet

The first step to running this demo is to launch your own `nuklaivm` Subnet. You
can do so by running the following command from this location (may take a few
minutes):

```bash
./scripts/run.sh;
```

When the Subnet is running, you'll see the following logs emitted:

```
cluster is ready!
avalanche-network-runner is running in the background...

use the following command to terminate:

./scripts/stop.sh;
```

_By default, this allocates all funds on the network to `nuklai1rvzhmceq997zntgvravfagsks6w0ryud3rylh4cdvayry0dl97ns9cs04w`. The private
key for this address is `0x323b1d8f4eed5f0da9da93071b034f2dce9d2d22692c172f3cb252a64ddfafd01b057de320297c29ad0c1f589ea216869cf1938d88c9fbd70d6748323dbf2fa7`.
For convenience, this key has is also stored at `demo.pk`._

### Build `nuklai-cli`

To make it easy to interact with the `nuklaivm`, we implemented the `nuklai-cli`.
Next, you'll need to build this tool. You can use the following command:

```bash
./scripts/build.sh
```

_This command will put the compiled CLI in `./build/nuklai-cli`._

### Configure `nuklai-cli`

Next, you'll need to add the chains you created and the default key to the
`nuklai-cli`. You can use the following commands from this location to do so:

```bash
./build/nuklai-cli key import demo.pk
```

If the key is added correctly, you'll see the following log:

```
database: .nuklai-cli
imported address: nuklai1rvzhmceq997zntgvravfagsks6w0ryud3rylh4cdvayry0dl97ns9cs04w
```

Next, you'll need to store the URLs of the nodes running on your Subnet:

```bash
./build/nuklai-cli chain import-anr
```

If `nuklai-cli` is able to connect to ANR, it will emit the following logs:

```
database: .nuklai-cli
stored chainID: 22vvdk7MECmY5bRE5nZXfTXuadAaZ476bPjodW8i4ubAVrimdt uri: http://127.0.0.1:34309/ext/bc/22vvdk7MECmY5bRE5nZXfTXuadAaZ476bPjodW8i4ubAVrimdt
stored chainID: 22vvdk7MECmY5bRE5nZXfTXuadAaZ476bPjodW8i4ubAVrimdt uri: http://127.0.0.1:39093/ext/bc/22vvdk7MECmY5bRE5nZXfTXuadAaZ476bPjodW8i4ubAVrimdt
stored chainID: 22vvdk7MECmY5bRE5nZXfTXuadAaZ476bPjodW8i4ubAVrimdt uri: http://127.0.0.1:38229/ext/bc/22vvdk7MECmY5bRE5nZXfTXuadAaZ476bPjodW8i4ubAVrimdt
stored chainID: 22vvdk7MECmY5bRE5nZXfTXuadAaZ476bPjodW8i4ubAVrimdt uri: http://127.0.0.1:33223/ext/bc/22vvdk7MECmY5bRE5nZXfTXuadAaZ476bPjodW8i4ubAVrimdt
stored chainID: 22vvdk7MECmY5bRE5nZXfTXuadAaZ476bPjodW8i4ubAVrimdt uri: http://127.0.0.1:46883/ext/bc/22vvdk7MECmY5bRE5nZXfTXuadAaZ476bPjodW8i4ubAVrimdt
```

_`./build/nuklai-cli chain import-anr` connects to the Avalanche Network Runner server running in
the background and pulls the URIs of all nodes tracking each chain you
created._

### Check Balance

To confirm you've done everything correctly up to this point, run the
following command to get the current balance of the key you added:

```bash
./build/nuklai-cli key balance
```

If successful, the balance response should look like this:

```
database: .nuklai-cli
address: nuklai1rvzhmceq997zntgvravfagsks6w0ryud3rylh4cdvayry0dl97ns9cs04w
chainID: 22vvdk7MECmY5bRE5nZXfTXuadAaZ476bPjodW8i4ubAVrimdt
uri: http://127.0.0.1:34309/ext/bc/22vvdk7MECmY5bRE5nZXfTXuadAaZ476bPjodW8i4ubAVrimdt
balance: 10000000000.000000000 NAI
```

### Generate Another Address

Now that we have a balance to send, we need to generate another address to send to. Because
we use bech32 addresses, we can't just put a random string of characters as the recipient
(won't pass checksum test that protects users from sending to off-by-one addresses).

```bash
./build/nuklai-cli key generate
```

If successful, the `nuklai-cli` will emit the new address:

```
database: .nuklai-cli
created address: nuklai15n2artc202ce3eh5umpjj84ess0u22sumrsnt6jp0p6hdp0jlrmsuzkxmj
```

By default, the `nuklai-cli` sets newly generated addresses to be the default. We run
the following command to set it back to `demo.pk`:

```bash
./build/nuklai-cli key set
```

You should see something like this:

```
database: .nuklai-cli
chainID: 22vvdk7MECmY5bRE5nZXfTXuadAaZ476bPjodW8i4ubAVrimdt
stored keys: 2
0) address: nuklai1rvzhmceq997zntgvravfagsks6w0ryud3rylh4cdvayry0dl97ns9cs04w balance: 10000000000.000000000 NAI
1) address: nuklai15n2artc202ce3eh5umpjj84ess0u22sumrsnt6jp0p6hdp0jlrmsuzkxmj balance: 0.000000000 NAI
set default key: 0
```

### Send Tokens

Lastly, we trigger the transfer:

```bash
./build/nuklai-cli action transfer
```

The `nuklai-cli` will emit the following logs when the transfer is successful:

```
database: .nuklai-cli-(1063)-> ./build/nuklai-cli action transfer
address: nuklai1rvzhmceq997zntgvravfagsks6w0ryud3rylh4cdvayry0dl97ns9cs04w
chainID: WCdnLx8mAbFoCpUqNn87kHaf5ENuxjLSQYLZNWyARrkp2ph6H
✔ recipient: nuklai15n2artc202ce3eh5umpjj84ess0u22sumrsnt6jp0p6hdp0jlrmsuzkxmj█
amount: 100
✔ continue (y/n): y█
✅ txID: 5xH1km8ZF1NKPh7Ab6H29BXy2NCD866anCYWSvHA3MLYxcehT
```

### Bonus: Watch Activity in Real-Time

To provide a better sense of what is actually happening on-chain, the
`nuklai-cli` comes bundled with a simple explorer that logs all blocks/txs that
occur on-chain. You can run this utility by running the following command from
this location:

```bash
./build/nuklai-cli chain watch
```

If you run it correctly, you'll see the following input (will run until the
network shuts down or you exit):

```
database: .nuklai-cli
available chains: 1 excluded: []
0) chainID: WCdnLx8mAbFoCpUqNn87kHaf5ENuxjLSQYLZNWyARrkp2ph6H
select chainID: 0 [auto-selected]
uri: http://127.0.0.1:40101/ext/bc/WCdnLx8mAbFoCpUqNn87kHaf5ENuxjLSQYLZNWyARrkp2ph6H
watching for new blocks on WCdnLx8mAbFoCpUqNn87kHaf5ENuxjLSQYLZNWyARrkp2ph6H 👀
height:128 txs:0 root:NrNLFPTKg7KwCPkKN3TP1NeFoCzaPEH5WBwHLyWxhe7n73zHi size:0.09KB units consumed: [bandwidth=0 compute=0 storage(read)=0 storage(create)=0 storage(modify)=0] unit prices: [bandwidth=100 compute=100 storage(read)=100 storage(create)=100 storage(modify)=100]
✅ 5xH1km8ZF1NKPh7Ab6H29BXy2NCD866anCYWSvHA3MLYxcehT actor: nuklai1rvzhmceq997zntgvravfagsks6w0ryud3rylh4cdvayry0dl97ns9cs04w summary (*actions.Transfer): [500.000000000 NAI -> nuklai15n2artc202ce3eh5umpjj84ess0u22sumrsnt6jp0p6hdp0jlrmsuzkxmj] fee (max 72.26%): 0.000023700 NAI consumed: [bandwidth=190 compute=7 storage(read)=14 storage(create)=0 storage(modify)=26]
```

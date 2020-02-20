// Copyright 2015 The go-simplechain Authors
// This file is part of the go-simplechain library.
//
// The go-simplechain library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-simplechain library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-simplechain library. If not, see <http://www.gnu.org/licenses/>.

package params

// MainnetBootnodes are the enode URLs of the P2P bootstrap nodes running on
// the main Simplechain network.
var MainnetBootnodes = []string{
	// SimpleChain Foundation Go Bootnodes
	//"enode://b9f34d999d0a719967f2b3e55f34b3938a9ff4c0c87e8064a3cd4102ad54ea89834f881177ffa0759e298c3e7e561426d366183836d8c81b0c7fb520fedf73db@47.89.225.204:30312",  // US
	//"enode://0681859a0f82761367b30ff517c32d20c6ddd8fd4140eb874051a4e9a66a7905e0f6bb6ed801cf57ec062f6ba4701643ee86ce51e8973c2795dd1e08772b3525@47.74.66.59:30312",    // AU
	//"enode://9a5f410b4b789494d4d703b85f84532167a1797abc5b60a68d79d35d208dee879c2bfbd76606aa7df8eefc6b2ee4e866c465397dd16e68ea0e1dc408a0e14357@47.91.16.204:30312",   // JPN
	//"enode://59ab117b157cb7afa6b9525ebcc354ba768e015f07161620f5d51e5bdc4c91791b8c7190df3016535618da97f2f79793d59eb337287e3228b2476f531e065bc0@47.254.153.168:30312", // DE
	//"enode://fbe579f10bed5edf6c17234a23861f2a56f9c4ecffde3dd098fcaab594a0bfa401750ca80fe5be0634e67f16ff7bb2c714196f67be80a6ca310eedcbfb56f0e5@47.99.136.157:30312",  // CN
	//"enode://c65ee620f96006ad30a058d37d95d73d4ebc2755bedb5ec49fc2c9709448072333cde5c875716f47819247517b3a3a3df3c958ca5302c80c135f165f7417f41d@47.99.133.102:30312",  // CN2
	//"enode://d10c77cbf098819af74c0afba3bdbb991bb72624b0f92ff7c3e1b7282906bbeac0087e77364b5bcb92ef55193c20641e582196e9763116976d0a44fc1d784ff9@118.31.45.65:30312",   // CN3
	"enode://89450e3a1f49783d053d925bdea62d0a121e635282e033e41fd55c7930883c443a771e16df00a599c9689852ad19876f50d3a57db5a647652c4d0c2bbf31883a@127.0.0.1:30234",
}

// TestnetBootnodes are the enode URLs of the P2P bootstrap nodes running on the
// test network.
var TestnetBootnodes = []string{
	"enode://bc6858a4de55d8715834a203def74162474e6ff8062c30093def22d577bcc96cd4755e9738be6b0c7e6f3ee7fcec5cc84a7c94b509692737e0744ada8bbde507@47.110.48.207:30312", // CN
	"enode://c72b5cb21086dac58bb9235bc68b217475e050e9c8c2a827867242193deb68a9c6abe13fd8da7cb64d3c2eb1d7ce6e4cdf5f48cd174b772934ef2446a21136a8@47.74.52.42:30312",   // JPN
	"enode://2e1162b335c72cfd767d2dffe617df942b9f71817557fffb28b24bff2aff5f2a18881ec7b58578498985400816e3fd62dcceed8cf842b9fd7dfa2fcbb464dea0@47.88.58.252:30312",  // US
}

// DiscoveryV5Bootnodes are the enode URLs of the P2P bootstrap nodes for the
// experimental RLPx v5 topic-discovery network.
var DiscoveryV5Bootnodes = []string{
	//"enode://06051a5573c81934c9554ef2898eb13b33a34b94cf36b202b69fde139ca17a85051979867720d4bdae4323d4943ddf9aeeb6643633aa656e0be843659795007a@35.177.226.168:30303",
	//"enode://0cc5f5ffb5d9098c8b8c62325f3797f56509bff942704687b6530992ac706e2cb946b90a34f1f19548cd3c7baccbcaea354531e5983c7d1bc0dee16ce4b6440b@40.118.3.223:30304",
	//"enode://1c7a64d76c0334b0418c004af2f67c50e36a3be60b5e4790bdac0439d21603469a85fad36f2473c9a80eb043ae60936df905fa28f1ff614c3e5dc34f15dcd2dc@40.118.3.223:30306",
	//"enode://85c85d7143ae8bb96924f2b54f1b3e70d8c4d367af305325d30a61385a432f247d2c75c45c6b4a60335060d072d7f5b35dd1d4c45f76941f62a4f83b6e75daaf@40.118.3.223:30307",
}

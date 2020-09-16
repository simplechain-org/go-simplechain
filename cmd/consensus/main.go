package main

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/big"
	"net"
	"os"
	"strconv"

	"github.com/simplechain-org/go-simplechain/accounts"
	"github.com/simplechain-org/go-simplechain/accounts/keystore"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/p2p/enode"
	"github.com/simplechain-org/go-simplechain/params"
	"github.com/simplechain-org/go-simplechain/rlp"

	"gopkg.in/urfave/cli.v1"
)

var app = cli.NewApp()

func init() {
	app.Commands = []cli.Command{
		dposCommand, raftCommand, pbftCommand, poaCommand,
	}
}

// consensus newnode --n=2 --nodedir=nodetest \
// --ip=127.0.0.1 --port=21000 --discport=0 --raftport=50040 \
// --ip=127.0.0.1 --port=21001 --discport=0 --raftport=50041
func main() {
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(consensus ConsensusType, n int, ips []string, ports []int, discports []int, raftports []int, nodeDir, genesis string) (e error) {
	fmt.Println("generate nodekey", "consensus", consensus, "n", n, "ips", ips, "ports", ports,
		"discports", discports, "raftports", raftports, "nodedir", nodeDir, "genesis", genesis)

	switch consensus {
	case RAFT:
		if len(ips) != n || len(ports) != n || len(discports) != n || len(raftports) != n {
			return fmt.Errorf("raft require %d ip, port, discport, raftport", n)
		}
	case PBFT:
		if len(ips) != n || len(ports) != n {
			return fmt.Errorf("istanbul require %d ip, port", n)
		}
	case DPOS, POA:
		//require nothing

	default:
		return fmt.Errorf("invalid consensus %d", consensus)
	}

	nodeKeys := make([]*ecdsa.PrivateKey, n)
	enodes := make([]string, n)

	for i := 0; i < n; i++ {
		prikey, err := crypto.GenerateKey()
		if err != nil {
			return err
		}

		nodeKeys[i] = prikey

		//if consensus == DPOS || consensus == POA {
		//	continue
		//}

		ip := net.ParseIP(ips[i])
		if ip == nil {
			return fmt.Errorf("invalid ip address %s", ips[i])
		}

		port := ports[i]
		if port < 0 || port > 65535 {
			return fmt.Errorf("invalid port %d", port)
		}

		var discport, raftport int
		// append raftport to enode key
		if consensus == RAFT {
			discport = discports[i]
			if discport < 0 || discport > 65535 {
				return fmt.Errorf("invalid discport %d", discport)
			}

			raftport = raftports[i]
			if raftport < 0 || raftport > 65535 {
				return fmt.Errorf("invalid raftport %d", raftport)
			}
		}

		enodes[i] = enode.NewV4Hostname(&prikey.PublicKey, ips[i], port, discport, raftport).String()
	}

	if err := mkdir(nodeDir); err != nil {
		return err
	}

	defer func() {
		if e != nil {
			os.RemoveAll(nodeDir)
		}
	}()

	accs, err := writeKeystore(nodeKeys, nodeDir)
	if err != nil {
		return err
	}

	if err := writeGenesis(consensus, accs, nodeDir, genesis); err != nil {
		return err
	}

	if err := writeNode(nodeKeys, nodeDir); err != nil {
		return err
	}

	if err := writeEnode(enodes, nodeDir); err != nil {
		return err
	}

	return
}

const (
	KeyFile         = "nodekey"
	KeyStore        = "keys"
	NodeFile        = "static-nodes.json"
	GenesisDPoSFile = "genesis_dpos.json"
	GenesisRaftFile = "genesis_raft.json"
	GenesisPbftFile = "genesis_pbft.json"
	GenesisPoaFile  = "genesis_poa.json"
)

func mkdir(dir string) error {
	stat, err := os.Stat(dir)
	if os.IsNotExist(err) {
		err = os.Mkdir(dir, 0700)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if stat != nil {
		return fmt.Errorf("%s is already exist", dir)
	}

	return nil
}

func writeNode(nodeKeys []*ecdsa.PrivateKey, dir string) error {
	for i, key := range nodeKeys {
		keyFile := KeyFile + strconv.Itoa(i+1)
		keyStr := hex.EncodeToString(crypto.FromECDSA(key))
		if err := writeFile(dir, keyFile, []byte(keyStr)); err != nil {
			return fmt.Errorf("write key file failed, %s", err.Error())
		}
	}

	return nil
}

func writeEnode(enodes []string, dir string) error {
	jsonByte := bytes.NewBuffer([]byte{})
	jsonEncoder := json.NewEncoder(jsonByte)
	jsonEncoder.SetEscapeHTML(false)
	if err := jsonEncoder.Encode(enodes); err != nil {
		return fmt.Errorf("write failed %s", err.Error())
	}

	if err := writeFile(dir, NodeFile, jsonByte.Bytes()); err != nil {
		return fmt.Errorf("write enode file failed, %s", err.Error())
	}

	return nil
}

func writeFile(dir, name string, content []byte) error {
	file, err := os.Create(dir + "/" + name)
	if err != nil {
		return err
	}

	if _, err = file.Write(content); err != nil {
		return err
	}
	return file.Close()
}

func writeGenesis(consensus ConsensusType, addresses []accounts.Account, dir, file string) error {
	gf, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("open genesis file failed, %s", err.Error())
	}

	b, err := ioutil.ReadAll(gf)
	if err != nil {
		return fmt.Errorf("read genesis file failed, %s", err.Error())
	}

	var genesis core.Genesis
	if err := json.Unmarshal(b, &genesis); err != nil {
		return fmt.Errorf("unmarshal genesis file failed, %s", err.Error())
	}

	if consensus == RAFT || consensus == DPOS || consensus == PBFT || consensus == POA {
		for _, addr := range addresses {
			genesis.Alloc[addr.Address] = core.GenesisAccount{Balance: new(big.Int).Mul(big.NewInt(params.Ether), big.NewInt(1e4))}
		}
	}

	var GenesisFile string
	switch consensus {
	case RAFT:
		GenesisFile = GenesisRaftFile
	case PBFT:
		GenesisFile = GenesisPbftFile
		genesis.ExtraData, err = makeIstanbulExtra(addresses)
	case DPOS:
		GenesisFile = GenesisDPoSFile
		makeDPoSSigners(&genesis, addresses)
	case POA:
		GenesisFile = GenesisPoaFile
		genesis.ExtraData, err = makePoaExtra(addresses)
	}

	marshaled, err := json.Marshal(genesis)
	if err != nil {
		return fmt.Errorf("marshal genesis file failed, %s", err.Error())
	}

	if err = writeFile(dir, GenesisFile, marshaled); err != nil {
		return fmt.Errorf("write genesis file failed, %s", err.Error())
	}

	return nil
}

func writeKeystore(privKeys []*ecdsa.PrivateKey, dir string) ([]accounts.Account, error) {
	var (
		keysPath = dir + "/" + KeyStore
		accs     = make([]accounts.Account, len(privKeys))
		ks       = keystore.NewKeyStore(keysPath, keystore.StandardScryptN, keystore.StandardScryptP)
	)

	for i, key := range privKeys {
		acc, err := ks.ImportECDSA(key, "")
		if err != nil {
			return nil, err
		}
		if err := os.Rename(acc.URL.Path, fmt.Sprintf("%s/key%d", keysPath, i+1)); err != nil {
			return nil, err
		}
		accs[i] = acc
	}

	return accs, nil
}

func makeDPoSSigners(genesis *core.Genesis, accs []accounts.Account) {
	for _, acc := range accs {
		genesis.Config.DPoS.SelfVoteSigners = append(genesis.Config.DPoS.SelfVoteSigners, common.UnprefixedAddress(acc.Address))
	}
}

func makePoaExtra(accs []accounts.Account) ([]byte, error) {
	var extra []byte

	const (
		extraVanity = 32                     // Fixed number of extra-data prefix bytes reserved for signer vanity
		extraSeal   = crypto.SignatureLength // Fixed number of extra-data suffix bytes reserved for signer seal
	)

	// compensate the lack bytes if header.Extra is not enough IstanbulExtraVanity bytes.
	if len(extra) < extraVanity {
		extra = append(extra, bytes.Repeat([]byte{0x00}, extraVanity-len(extra))...)
	}

	for _, acc := range accs {
		extra = append(extra, acc.Address[:]...)
	}
	extra = append(extra, make([]byte, extraSeal)...)
	return extra, nil
}

func makeIstanbulExtra(accs []accounts.Account) ([]byte, error) {
	var buf bytes.Buffer
	var extra []byte
	var addresses []common.Address
	for _, acc := range accs {
		addresses = append(addresses, acc.Address)
	}

	// compensate the lack bytes if header.Extra is not enough IstanbulExtraVanity bytes.
	if len(extra) < types.IstanbulExtraVanity {
		extra = append(extra, bytes.Repeat([]byte{0x00}, types.IstanbulExtraVanity-len(extra))...)
	}
	buf.Write(extra[:types.IstanbulExtraVanity])

	ist := &types.ByzantineExtra{
		Validators:    addresses,
		Seal:          []byte{},
		CommittedSeal: [][]byte{},
	}

	payload, err := rlp.EncodeToBytes(&ist)
	if err != nil {
		return nil, err
	}

	extra = append(buf.Bytes(), payload...)
	return extra, nil
}

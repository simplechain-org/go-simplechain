package db

import (
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/rawdb"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/ethdb"

	"github.com/asdine/storm/v3"
	"github.com/asdine/storm/v3/q"
)

type ErrCtxDbFailure struct {
	msg string
	err error
}

func (e ErrCtxDbFailure) Error() string {
	return fmt.Sprintf("DB ctx handle failed:%s %s", e.msg, e.err)
}

type ServiceContext interface {
	ResolvePath(string) string
	OpenDatabase(string, int, int, string) (ethdb.Database, error)
}

type CtxDB interface {
	io.Closer
	ChainID() *big.Int
	Count(filter ...q.Matcher) int
	Height() uint64
	Write(ctx *cc.CrossTransactionWithSignatures) error
	Writes([]*cc.CrossTransactionWithSignatures, bool) error
	Read(ctxId common.Hash) (*cc.CrossTransactionWithSignatures, error)
	Update(id common.Hash, updater func(ctx *CrossTransactionIndexed)) error
	Updates(idList []common.Hash, updaters []func(ctx *CrossTransactionIndexed)) error
	Deletes(idList []common.Hash) error
	Has(id common.Hash) bool

	One(field FieldName, key interface{}) *cc.CrossTransactionWithSignatures
	//Find(field FieldName, key interface{}) []*cc.CrossTransactionWithSignatures
	Query(pageSize int, startPage int, orderBy []FieldName, reverse bool, filter ...q.Matcher) []*cc.CrossTransactionWithSignatures
	RangeByNumber(begin, end uint64, limit int) []*cc.CrossTransactionWithSignatures

	Load() error
	Repair() error
	Clean() error
}

func OpenStormDB(ctx ServiceContext, name string) (*storm.DB, error) {
	if ctx == nil || len(ctx.ResolvePath(name)) == 0 {
		return storm.Open(filepath.Join(os.TempDir(), name))
	}
	return storm.Open(ctx.ResolvePath(name))
}

func OpenEtherDB(ctx ServiceContext, name string) (ethdb.Database, error) {
	if ctx == nil {
		return rawdb.NewMemoryDatabase(), nil
	}
	//TODO: cache & handles
	db, err := ctx.OpenDatabase(name, 16, 256, "")
	if err != nil {
		return nil, ErrCtxDbFailure{msg: "OpenEtherDB fail", err: err}
	}
	return db, nil
}

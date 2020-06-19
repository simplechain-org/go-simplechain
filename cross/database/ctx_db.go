package db

import (
	"fmt"
	"github.com/simplechain-org/go-simplechain/common"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/ethdb"
	"io"
	"math/big"
	"os"
	"path"

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
	Has(id common.Hash) bool

	One(field FieldName, key interface{}) *cc.CrossTransactionWithSignatures
	Find(field FieldName, key interface{}) []*cc.CrossTransactionWithSignatures
	Query(pageSize int, startPage int, orderBy []FieldName, reverse bool, filter ...q.Matcher) []*cc.CrossTransactionWithSignatures
	RangeByNumber(begin, end uint64, limit int) []*cc.CrossTransactionWithSignatures

	Load() error
	Repair() error
	Clean() error
}

func OpenStormDB(ctx ServiceContext, name string) (*storm.DB, error) {
	if ctx == nil || len(ctx.ResolvePath(name)) == 0 {
		return storm.Open(path.Join(os.TempDir(), name), storm.BoltOptions(0700, nil))
	}

	return storm.Open(ctx.ResolvePath(name))
}

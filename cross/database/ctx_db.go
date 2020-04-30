package db

import (
	"fmt"
	"io"
	"os"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/rawdb"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/ethdb"

	"github.com/asdine/storm/v3"
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
	Size() int
	Load() error
	Write(ctx *types.CrossTransactionWithSignatures) error
	Read(ctxId common.Hash) (*types.CrossTransactionWithSignatures, error)
	ReadAll(ctxId common.Hash) (common.Hash, error)
	Delete(ctxId common.Hash) error
	Update(id common.Hash, updater func(ctx *CrossTransactionIndexed)) error
	Has(id common.Hash) bool
	QueryByPK(pageSize int, startPage int, filter ...interface{}) []*types.CrossTransactionWithSignatures
	QueryByPrice(pageSize int, startPage int, filter ...interface{}) []*types.CrossTransactionWithSignatures
	Range(pageSize int, startCtxID *common.Hash, endCtxID *common.Hash) []*types.CrossTransactionWithSignatures
}

func OpenStormDB(ctx ServiceContext, name string) (*storm.DB, error) {
	if ctx == nil {
		return storm.Open(os.TempDir() + name)
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

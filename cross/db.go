package cross

//var statementPrefix = []byte("statement_")

var txPrefix = []byte("tx_")

var recordPrefix = []byte("record_")

var recordIndex = []byte("index")

var maxRecord uint64=10000

func txKey(id []byte) []byte {
	return append(txPrefix, id...)
}

func recordKey(id []byte) []byte {
	return append(recordPrefix, id...)
}

//func statementKey(id []byte) []byte {
//	return append(statementPrefix, id...)
//}

//type StatementDb struct {
//	db ethdb.Database
//	lock sync.Mutex
//}
//
//func NewStatementDb(db ethdb.Database) *StatementDb {
//	return &StatementDb{db: db}
//}
//
//func (this *StatementDb) Write(state *types.Statement) error {
//	if state == nil {
//		return errors.New("statement is nil")
//	}
//	enc, err := rlp.EncodeToBytes(state)
//	if err != nil {
//		return err
//	}
//	this.lock.Unlock()
//	defer this.lock.Unlock()
//	index:=this.getIndex()
//	index=index.Add(index,big.NewInt(1))
//	err=this.db.Put(txKey(state.CtxId[:]),index.Bytes())
//	if err != nil {
//		return err
//	}
//	err=this.db.Put(recordKey(index.Bytes()), enc)
//	if err != nil {
//		return err
//	}
//	err=this.updateIndex(index)
//	if err != nil {
//		return err
//	}
//	return nil
//}
//
//func (this *StatementDb) Read(ctxId common.Hash) (*types.Statement, error) {
//	data, err := this.db.Get(txKey(ctxId[:]))
//	if err != nil {
//		return nil, err
//	}
//	d, err := this.db.Get(recordKey(data[:]))
//	if err != nil {
//		return nil, err
//	}
//	state := new(types.Statement)
//	err = rlp.Decode(bytes.NewReader(d), state)
//	if err != nil {
//		return nil, err
//	}
//	return state, nil
//}
//
//func (this *StatementDb) ListAll() ([]*types.Statement, error) {
//	result := make([]*types.Statement, 0)
//	if db, ok := this.db.(*ethdb.LDBDatabase); ok {
//		it := db.NewIteratorWithPrefix(recordPrefix)
//		for it.Next() {
//			state := new(types.Statement)
//			err := rlp.Decode(bytes.NewReader(it.Value()), state)
//			if err == nil {
//				result = append(result, state)
//			}
//		}
//	}
//	return result, nil
//}
//func (this *StatementDb) Has(ctxId common.Hash) bool {
//	if db, ok := this.db.(*ethdb.LDBDatabase); ok {
//		has, err := db.Has(txKey(ctxId[:]))
//		if err != nil {
//			return false
//		}
//		return has
//	}
//	return false
//}
//
//func (this *StatementDb) getIndex() *big.Int{
//	if db, ok := this.db.(*ethdb.LDBDatabase); ok {
//		has, err := db.Has(recordIndex)
//		if err != nil {
//			return  big.NewInt(0)
//		}
//		if has{
//			data, err := this.db.Get(recordIndex)
//			if err != nil {
//				return  big.NewInt(0)
//			}
//			return big.NewInt(0).SetBytes(data)
//		}else {
//			return  big.NewInt(0)
//	  }
//	}else{
//		return big.NewInt(0)
//	}
//}
//
//func (this *StatementDb) updateIndex(index *big.Int) error{
//	return this.db.Put(recordIndex, index.Bytes())
//}
//
//func (this *StatementDb) List(start *big.Int,count uint64) ([]*types.Statement, error) {
//	result := make([]*types.Statement, 0)
//	var i uint64=0
//	for ;i<count;i++{
//		data:=start.Add(start,big.NewInt(0).SetUint64(i))
//		d, err := this.db.Get(recordKey(data.Bytes()))
//		if err != nil {
//			return nil, err
//		}
//		state := new(types.Statement)
//		err = rlp.Decode(bytes.NewReader(d), state)
//		if err != nil {
//			return nil, err
//		}
//		result=append(result,state)
//	}
//	return result, nil
//}




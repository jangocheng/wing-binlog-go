package binlog

import (
	"math/big"
	"reflect"
	"strconv"
	"sync/atomic"
	"time"
	"library/services"
	"github.com/siddontang/go-mysql/canal"
	"github.com/siddontang/go-mysql/mysql"
	"github.com/siddontang/go-mysql/replication"
	"github.com/siddontang/go-mysql/schema"
	log "github.com/sirupsen/logrus"
	"os"
	"library/path"
)

func (h *Binlog) handlerInit() {
	var (
		b   [defaultBufferSize]byte
		err error
	)
	mysqlBinlogCacheFile := path.CurrentPath + "/cache/mysql_binlog_position.pos"
	path.Mkdir(path.GetParent(mysqlBinlogCacheFile))
	flag := os.O_RDWR | os.O_CREATE | os.O_SYNC // | os.O_TRUNC
	h.cacheHandler, err = os.OpenFile(mysqlBinlogCacheFile, flag, 0755)
	if err != nil {
		log.Panicf("binlog open cache file error：%s, %+v", mysqlBinlogCacheFile, err)
	}
	f, p, index := h.getBinlogPositionCache()
	h.EventIndex = index
	h.buf        = b[:0]
	h.isClosed   = true
	h.setHandler()
	currentPos, err := h.handler.GetMasterPos()
	if f != "" {
		h.Config.BinFile = f
	} else {
		if err != nil {
			log.Panicf("binlog get cache error：%+v", err)
		} else {
			h.Config.BinFile = currentPos.Name
		}
	}
	if p > 0 {
		h.Config.BinPos = uint32(p)
	} else {
		if err != nil {
			log.Panicf("binlog get cache error：%+v", err)
		} else {
			h.Config.BinPos = currentPos.Pos
		}
	}
	h.lastBinFile = h.Config.BinFile
	h.lastPos = uint32(h.Config.BinPos)
}

func (h *Binlog) setHandler()  {
	cfg, err := canal.NewConfigWithFile(path.CurrentPath + "/config/canal.toml")
	if err != nil {
		log.Panicf("binlog create canal config error：%+v", err)
	}
	handler, err := canal.NewCanal(cfg)
	if err != nil {
		log.Panicf("binlog create canal error：%+v", err)
	}
	h.handler = handler
	h.handler.SetEventHandler(h)
}

func (h *Binlog) RegisterService(name string, s services.Service) {
	h.services[name] = s
}

func (h *Binlog) notify(msg []byte) {
	log.Debug("binlog发送广播：", msg, string(msg))
	for _, service := range h.services {
		service.SendAll(msg)
	}
}

func (h *Binlog) append(buf *[]byte, edata interface{}, column *schema.TableColumn) {
	//log.Debugf("%+v,===,%+v, == %+v", column, reflect.TypeOf(edata), edata)
	switch edata.(type) {
	case string:
		encode(buf, edata.(string))
	case []uint8:
		encode(buf, string(edata.([]byte)))
	case int:
		*buf = strconv.AppendInt(*buf, int64(edata.(int)), 10)
	case int8:
		var r int64 = 0
		r = int64(edata.(int8))
		if column.IsUnsigned && r < 0 {
			r = int64(int64(256) + int64(edata.(int8)))
		}
		*buf = strconv.AppendInt(*buf, r, 10)
	case int16:
		var r int64 = 0
		r = int64(edata.(int16))
		if column.IsUnsigned && r < 0 {
			r = int64(int64(65536) + int64(edata.(int16)))
		}
		*buf = strconv.AppendInt(*buf, r, 10)
	case int32:
		var r int64 = 0
		r = int64(edata.(int32))
		if column.IsUnsigned && r < 0 {
			t := string([]byte(column.RawType)[0:3])
			if t != "int" {
				r = int64(int64(1<<24) + int64(edata.(int32)))
			} else {
				r = int64(int64(4294967296) + int64(edata.(int32)))
			}
		}
		*buf = strconv.AppendInt(*buf, r, 10)
	case int64:
		// 枚举类型支持
		if len(column.RawType) > 4 && column.RawType[0:4] == "enum" {
			i := int(edata.(int64)) - 1
			str := column.EnumValues[i]
			encode(buf, str)
		} else if len(column.RawType) > 3 && column.RawType[0:3] == "set" {
			v := uint(edata.(int64))
			l := uint(len(column.SetValues))
			res := ""
			for i := uint(0); i < l; i++ {
				if (v & (1 << i)) > 0 {
					if res != "" {
						res += ","
					}
					res += column.SetValues[i]
				}
			}
			encode(buf, res)
		} else {
			if column.IsUnsigned {
				var ur uint64 = 0
				ur = uint64(edata.(int64))
				if ur < 0 {
					ur = 1<<63 + (1<<63 + ur)
				}
				*buf = strconv.AppendUint(*buf, ur, 10)
			} else {
				*buf = strconv.AppendInt(*buf, int64(edata.(int64)), 10)
			}
		}
	case uint:
		*buf = strconv.AppendUint(*buf, uint64(edata.(uint)), 10)
	case uint8:
		*buf = strconv.AppendUint(*buf, uint64(edata.(uint8)), 10)
	case uint16:
		*buf = strconv.AppendUint(*buf, uint64(edata.(uint16)), 10)
	case uint32:
		*buf = strconv.AppendUint(*buf, uint64(edata.(uint32)), 10)
	case uint64:
		*buf = strconv.AppendUint(*buf, uint64(edata.(uint64)), 10)
	case float64:
		f := big.NewFloat(edata.(float64))
		*buf = append(*buf, f.String()...)
	case float32:
		f := big.NewFloat(float64(edata.(float32)))
		*buf = append(*buf, f.String()...)
	default:
		if edata != nil {
			log.Warnf("binlog不支持的类型：%s %+v", column.Name /*col.Name*/, reflect.TypeOf(edata))
			*buf = append(*buf, "\"--unkonw type--\""...)
		} else {
			*buf = append(*buf, "null"...)
		}
	}
}

func (h *Binlog) OnRow(e *canal.RowsEvent) error {
	// 发生变化的数据表e.Table，如xsl.x_reports
	// 发生的操作类型e.Action，如update、insert、delete
	// 如update的数据，update的数据以双数出现前面为更新前的数据，后面的为更新后的数据
	// 0，2，4偶数的为更新前的数据，奇数的为更新后的数据
	// [[1 1 3074961 [115 102 103 98 114]   1 1485739538 1485739538]
	// [1 1 3074961 [115 102 103 98 114] 1 1 1485739538 1485739538]]
	// delete一次返回一条数据
	// delete的数据delete [[3 1 3074961 [97 115 100 99 97 100 115] 1,2,2 1 1485768268 1485768268]]
	// 一次插入多条的时候，同时返回
	// insert的数据insert xsl.x_reports [[6 0 0 [] 0 1 0 0]]
	log.Debugf("binlog基础数据：%+v", e.Rows)
	columns_len := len(e.Table.Columns)
	log.Debugf("binlog缓冲区详细信息: %d %d", len(h.buf), cap(h.buf))
	db := []byte(e.Table.Schema)
	point := []byte(".")
	table := []byte(e.Table.Name)
	dblen := len(db) + len(table) + len(point)
	if e.Action == "update" {
		for i := 0; i < len(e.Rows); i += 2 {
			atomic.AddInt64(&h.EventIndex, int64(1))
			buf := h.buf[:0]
			buf = append(buf, byte(dblen))
			buf = append(buf, byte(dblen>>8))
			buf = append(buf, db...)
			buf = append(buf, point...)
			buf = append(buf, table...)
			buf = append(buf, "{\"database\":\""...)
			buf = append(buf, e.Table.Schema...)
			buf = append(buf, "\",\"event\":{\"data\":{\"old_data\":{"...)
			rows_len := len(e.Rows[i])
			for k, col := range e.Table.Columns {
				buf = append(buf, "\""...)
				buf = append(buf, col.Name...)
				buf = append(buf, "\":"...)
				var edata interface{}
				if k < rows_len {
					edata = e.Rows[i][k]
				} else {
					log.Warn("binlog未知的行", col.Name)
					edata = nil
				}
				h.append(&buf, edata, &col)
				if k < columns_len-1 {
					buf = append(buf, ","...)
				}
			}
			buf = append(buf, "},\"new_data\":{"...)
			rows_len = len(e.Rows[i+1])
			for k, col := range e.Table.Columns {
				buf = append(buf, "\""...)
				buf = append(buf, col.Name...)
				buf = append(buf, "\":"...)
				var edata interface{}
				if k < rows_len {
					edata = e.Rows[i+1][k]
				} else {
					log.Warn("binlog未知的行", col.Name)
					edata = nil
				}
				h.append(&buf, edata, &col)
				if k < columns_len-1 {
					buf = append(buf, ","...)
				}
			}
			buf = append(buf, "}},\"event_type\":\""...)
			buf = append(buf, e.Action...)
			buf = append(buf, "\",\"time\":"...)
			buf = strconv.AppendInt(buf, time.Now().Unix(), 10)
			buf = append(buf, "},\"event_index\":"...)
			buf = strconv.AppendInt(buf, h.EventIndex, 10)
			buf = append(buf, ",\"table\":\""...)
			buf = append(buf, e.Table.Name...)
			buf = append(buf, "\"}"...)
			h.notify(buf)
		}
	} else {
		for i := 0; i < len(e.Rows); i += 1 {
			atomic.AddInt64(&h.EventIndex, int64(1))
			buf := h.buf[:0]
			buf = append(buf, byte(dblen))
			buf = append(buf, byte(dblen>>8))
			buf = append(buf, db...)
			buf = append(buf, point...)
			buf = append(buf, table...)
			buf = append(buf, "{\"database\":\""...)
			buf = append(buf, e.Table.Schema...)
			buf = append(buf, "\",\"event\":{\"data\":{"...)
			rows_len := len(e.Rows[i])
			for k, col := range e.Table.Columns {
				buf = append(buf, "\""...)
				buf = append(buf, col.Name...)
				buf = append(buf, "\":"...)
				var edata interface{}
				if k < rows_len {
					edata = e.Rows[i][k]
				} else {
					log.Warn("binlog未知的行", col.Name)
					edata = nil
				}
				h.append(&buf, edata, &col)
				if k < columns_len-1 {
					buf = append(buf, ","...)
				}
			}
			buf = append(buf, "},\"event_type\":\""...)
			buf = append(buf, e.Action...)
			buf = append(buf, "\",\"time\":"...)
			buf = strconv.AppendInt(buf, time.Now().Unix(), 10)
			buf = append(buf, "},\"event_index\":"...)
			buf = strconv.AppendInt(buf, h.EventIndex, 10)
			buf = append(buf, ",\"table\":\""...)
			buf = append(buf, e.Table.Name...)
			buf = append(buf, "\"}"...)
			h.notify(buf)
		}
	}
	return nil
}

func (h *Binlog) String() string {
	return "Binlog"
}

func (h *Binlog) OnRotate(e *replication.RotateEvent) error {
	log.Debugf("OnRotate event fired, %+v", e)
	return nil
}

func (h *Binlog) OnDDL(p mysql.Position, e *replication.QueryEvent) error {
	log.Debugf("OnDDL event fired, %+v, %+v", p, e)
	return nil
}

func (h *Binlog) OnXID(p mysql.Position) error {
	log.Debugf("OnXID event fired, %+v.", p)
	return nil
}

func (h *Binlog) OnGTID(g mysql.GTIDSet) error {
	log.Debugf("OnGTID event fired, GTID: %+v", g)
	return nil
}

func (h *Binlog) onPosChange(data []byte) {
	if data == nil {
		return
	}
	if len(data) < 19 {
		return
	}
	log.Debugf("onPos")
	// dl is file data length
	//pos := int64(data[2]) | int64(data[3])<<8 | int64(data[4])<<16 |
	//	int64(data[5])<<24 | int64(data[6])<<32 | int64(data[7])<<40 |
	//	int64(data[8])<<48 | int64(data[9])<<56
	//eventIndex := int64(data[10]) | int64(data[11])<<8 |
	//	int64(data[12])<<16 | int64(data[13])<<24 |
	//	int64(data[14])<<32 | int64(data[15])<<40 |
	//	int64(data[16])<<48 | int64(data[17])<<56
	//log.Debugf("onPos %s, %d, %d", string(data[18:]), pos, eventIndex)
	//
	file, pos, index := unpackPos(data)

	h.lastBinFile = file//string(data[18:])
	h.lastPos = uint32(pos)
	h.EventIndex = index//eventIndex
	h.SaveBinlogPostionCache(string(data[18:]), pos, index)
}


func (h *Binlog) OnPosSynced(p mysql.Position, b bool) error {
	log.Debugf("OnPosSynced fired with data: %+v %b", p, b)
	eventIndex := atomic.LoadInt64(&h.EventIndex)
	pos := int64(p.Pos)
	h.SaveBinlogPostionCache(p.Name, pos, eventIndex)
	r := packPos(p.Name, pos, eventIndex)
	h.Write(r)
	h.lastBinFile = p.Name
	h.lastPos = p.Pos
	return nil
}

func (h *Binlog) SaveBinlogPostionCache(binFile string, pos int64, eventIndex int64) {
	r := packPos(binFile, pos, eventIndex)
	log.Debugf("write binlog cache: %s, %d, %d, %+v", binFile, pos, eventIndex, r)
	n, err := h.cacheHandler.WriteAt(r, 0)
	log.Debugf("%d , %+v", n, err)
	if err != nil || n <= 0 {
		log.Errorf("binlog写入缓存文件错误：%+v", err)
		return
	}
}

func (h *Binlog) getBinlogPositionCache() (string, int64, int64) {
	// read 2 bytes is file data length
	l := make([]byte, 2)
	n, err := h.cacheHandler.Read(l)
	if n <= 0 || err != nil {
		return "", int64(0), int64(0)
	}
	// dl is file data length
	dl := int64(l[0]) | int64(l[1]) << 8
	data := make([]byte, dl)
	h.cacheHandler.Read(data)
	if len(data) < 18 {
		return "", 0, 0
	}
	pos := int64(data[0]) | int64(data[1]) << 8 | int64(data[2]) << 16 |
			int64(data[3]) << 24 | int64(data[4]) << 32 | int64(data[5]) << 40 |
			int64(data[6]) << 48 | int64(data[7]) << 56
	eventIndex := int64(data[8]) | int64(data[9]) << 8 |
			int64(data[10]) << 16 | int64(data[11]) << 24 |
			int64(data[12]) << 32 | int64(data[13]) << 40 |
			int64(data[14]) << 48 | int64(data[15]) << 56
	return string(data[16:]), pos, eventIndex
}

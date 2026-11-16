package completion

import (
	_ "embed"
	"sync"
)

//go:embed data/descriptions.zh-CN.gz
var chineseDescriptionData []byte

var chineseDescriptions struct {
	once  sync.Once
	table *Descriptions
	err   error
}

func BuiltinChineseDescriptions() (*Descriptions, error) {
	chineseDescriptions.once.Do(func() {
		chineseDescriptions.table, chineseDescriptions.err = DecodeDescriptions(chineseDescriptionData)
	})
	return chineseDescriptions.table, chineseDescriptions.err
}

package rag //coding

import (
	"context"

	"github.com/aydndglr/pars-agent-v3/internal/memory"
)

// OracleIndexTool: Sadece resmi dil dokümanlarını pars_docs.db içine gömmek için kullanılır.
type OracleIndexTool struct {
	DocStore *memory.SQLiteStore 
}

func (t *OracleIndexTool) Name() string { return "oracle_index" }

func (t *OracleIndexTool) Description() string {
    return "RESMİ DOKÜMAN YUTUCU: Sadece ve sadece Python, Go, React gibi dillerin resmî dokümanlarını 'pars_docs.db' içine kaydeder eğer kullanıcı --learn-docs parametresi girerse ya 'programmlama dili dökümanını kaydet' gibi talimat girerse 'pars_docs.db' dosyasına yaz . Kullanıcı projeleri için ASLA kullanma DİKKAT: Bu araç belirtilen klasörü KENDİSİ OKUR ve KENDİSİ KAYDEDER. Bu aracı kullandıktan sonra ASLA 'fs_index' aracını kullanma, işlem tek adımda biter. İşlem bitince görevi doğrudan sonlandır."
}

func (t *OracleIndexTool) Parameters() map[string]interface{} {
	baseIndexer := &IndexerTool{}
	return baseIndexer.Parameters()
}

func (t *OracleIndexTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	indexer := &IndexerTool{Store: t.DocStore} 
	return indexer.Execute(ctx, args)
}
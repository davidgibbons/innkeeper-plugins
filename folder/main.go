// Command folder is a catalog plugin over a folder of character cards and
// lorebooks.
package main

import (
	"github.com/davidgibbons/innkeeper-plugins/catalogkit"
	"github.com/davidgibbons/innkeeper/protocol"
)

const version = "0.1.0"

var catalogConfig = catalogkit.Config{
	Kinds: []string{protocol.KindCard, protocol.KindLorebook},
	Filters: []catalogkit.Filter{
		{CatalogFilter: protocol.CatalogFilter{Name: "subdirectory", Label: "Subfolder", Type: protocol.FilterText}, Field: "attrs.dir"},
		{CatalogFilter: protocol.CatalogFilter{Name: "tag", Label: "Tag", Type: protocol.FilterText}, Field: "tag"},
		{CatalogFilter: protocol.CatalogFilter{Name: "modified", Label: "Date modified", Type: protocol.FilterDateRange}, Field: "updated"},
	},
	Ingest: true,
	Migrations: []catalogkit.Migration{{Name: "folder/1-file",
		SQL: `CREATE TABLE file (path text PRIMARY KEY, mtime timestamptz NOT NULL, size bigint NOT NULL)`}},
}

// env is what initialize set up.
type env struct {
	store   *catalogkit.Store // nil without a database_url
	folder  string            // config.folder
	blobTmp string
}

func main() {}

package embedded

import _ "embed"

//go:embed catalog.json
var catalogJSON []byte

//go:embed catalog.json.sha256
var CatalogJSONSHA256 []byte

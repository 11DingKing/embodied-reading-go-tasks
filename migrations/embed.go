package migrations

import _ "embed"

// Initial contains the complete version-one schema used for new databases.
//
//go:embed 001_initial.sql
var Initial string

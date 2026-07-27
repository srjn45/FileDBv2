// embeddemo is a standalone module that embeds the ScrivaDB storage engine
// directly, importing only the public `engine` package. It exists to prove —
// and, via CI, to keep proving — that the engine can be linked into an
// application without dragging in the server's grpc/protobuf/prometheus/cobra
// dependency tree.
module github.com/srjn45/scriva/embeddemo

go 1.24.0

require github.com/srjn45/scriva v0.0.0

require (
	golang.org/x/crypto v0.46.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
)

// Build against the engine in this repository, not a published version.
replace github.com/srjn45/scriva => ../

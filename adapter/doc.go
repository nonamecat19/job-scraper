// Package adapter is the composition layer between the ports and the concrete
// sources in adapters/.
//
// It contains no scraping logic of its own. What it provides is the machinery
// for assembling sources into something a caller can drive:
//
//   - Registry keeps the configured sources, in a stable order, and resolves a
//     posting URL to whichever source claims it.
//   - Provider and Catalog are the factory layer: a source is described by a
//     Provider that knows its key and how to build it from Deps, and Catalog
//     collects those Providers so the set of available sources is data rather
//     than a hard-coded switch.
//   - Deps is the single bag of ports every source is constructed from, so
//     adding a dependency to one source does not change any other's signature.
//   - As unwraps a decorated source back to a capability interface, which is
//     what keeps the middleware in adapter/middleware transparent.
//
// The split matters because sources are wrapped: a registered JobSource is
// usually several decorators deep, and only As can see through that to ask
// whether the thing underneath can, say, read a single posting.
package adapter

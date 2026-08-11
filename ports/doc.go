// Package ports declares every interface the library is built around, plus the
// plain value types those interfaces exchange.
//
// The package is the pivot of the dependency inversion the library relies on:
// it imports nothing but the domain model and the standard library, and every
// other package depends on it rather than on a concrete implementation. A
// consumer that wants to replace a piece of the library — the retriever, the
// roster store, the HTTP scraper, a single job source — implements the port
// here and hands the implementation to jobscraper.New.
//
// The ports fall into four groups:
//
//   - Sources: JobSource and the optional capability interfaces a source may
//     also implement (DetailNeeder, Credentialed, PostingReader,
//     EmployerReporter). See source.go.
//   - Retrieval: Retriever, the page-fetching ladder, and StateStore, the
//     per-host memory it keeps. See retrieval.go and state.go.
//   - Storage: Roster, the employer-board roster a vendor source iterates.
//     See roster.go.
//   - Transport and sessions: Scraper, HTTPDoer, SessionProvider and
//     ConfigStore. See scraper.go and session.go.
//
// Observer declares the notification port used to watch a run in progress.
package ports

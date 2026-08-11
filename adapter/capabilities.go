package adapter

import "github.com/nonamecat19/job-scraper/ports"

// Unwrapper is implemented by every decorator in adapter/middleware. It is how
// As sees past the wrapping to the source underneath.
//
// A decorator that forgets to implement it silently hides the capabilities of
// everything it wraps, which is why the middleware package embeds a base type
// that provides it.
type Unwrapper interface {
	Unwrap() ports.JobSource
}

// As reports whether src — or anything it wraps — implements T, and returns it.
//
// It exists because a registered source is rarely the bare source: it is that
// source behind a retry decorator behind a logging decorator. Asking
// `src.(ports.PostingReader)` on the outermost wrapper answers "no" even when
// the source at the bottom can read postings perfectly well. As walks the
// Unwrap chain and asks each layer.
//
//	if pr, ok := adapter.As[ports.PostingReader](src); ok {
//		job, err := pr.ReadPosting(ctx, url, cfg)
//	}
func As[T any](src ports.JobSource) (T, bool) {
	for src != nil {
		if typed, ok := src.(T); ok {
			return typed, true
		}
		u, ok := src.(Unwrapper)
		if !ok {
			break
		}
		src = u.Unwrap()
	}
	var zero T
	return zero, false
}

// Base returns the innermost source, with every decorator stripped off. Use it
// for identity comparisons; use As for behaviour.
func Base(src ports.JobSource) ports.JobSource {
	for {
		u, ok := src.(Unwrapper)
		if !ok {
			return src
		}
		inner := u.Unwrap()
		if inner == nil {
			return src
		}
		src = inner
	}
}

// NeedsDetail reports whether src returns postings with incomplete
// descriptions, so the caller knows to run a detail pass.
func NeedsDetail(src ports.JobSource) bool {
	dn, ok := As[ports.DetailNeeder](src)
	return ok && dn.NeedsDetail()
}

// IsCredentialed reports whether src reads pages logged in as the end user.
// Retrieval never escalates such a source: swapping transport under a live
// session invalidates it.
func IsCredentialed(src ports.JobSource) bool {
	c, ok := As[ports.Credentialed](src)
	return ok && c.UsesUserAccount()
}

// AsPostingReader returns src's PostingReader, if it has one.
func AsPostingReader(src ports.JobSource) (ports.PostingReader, bool) {
	return As[ports.PostingReader](src)
}

// AsJobDetailReader returns src's JobDetailReader, if it has one. A source
// without one can still produce a JobDetail through the client, projected from
// its PostingReader.
func AsJobDetailReader(src ports.JobSource) (ports.JobDetailReader, bool) {
	return As[ports.JobDetailReader](src)
}

// AsEmployerReporter returns src's EmployerReporter, if it has one.
func AsEmployerReporter(src ports.JobSource) (ports.EmployerReporter, bool) {
	return As[ports.EmployerReporter](src)
}

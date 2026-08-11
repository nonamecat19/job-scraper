package retrieval

import (
	"net/http"
	"time"

	"github.com/nonamecat19/jobscraper/internal/httpjson"
)

// PacedClient is the http.Client the API-kind sources fetch through. It is an
// ordinary client over DefaultTransport, so the JSON path queues behind the
// same per-host limiters the retrieval ladder consults.
//
// The two paths are separate code but not separate to the host being read: a
// site that asked for one request every four seconds means the total, not one
// budget for HTML and another for its JSON API. Sharing DefaultTransport is
// what keeps the two honest about each other.
var PacedClient = &http.Client{Transport: DefaultTransport, Timeout: 30 * time.Second}

// Importing retrieval is what puts the JSON helper on the paced client. The
// library installs it rather than leaving it to the consumer because httpjson
// lives under internal/ — nothing outside this module can reach its default
// client, so nobody else is in a position to pace it.
//
// The dependency runs one way only: retrieval knows about httpjson, httpjson
// knows nothing about retrieval, and a JSON source keeps taking a plain
// *http.Client it does not have to understand.
func init() { httpjson.SetDefaultClient(PacedClient) }

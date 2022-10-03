package providers

import (
	"fmt"
	"github.com/go-logr/logr"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lwolf/konsumerator/api/v1"
)

func TestNewPrometheusMP(t *testing.T) {
	testCases := []struct {
		name   string
		addrs  []string
		expErr string
	}{
		{
			"no hosts",
			[]string{},
			allConsFailedErr.Error(),
		},
		{
			"broken host",
			[]string{"%%22.%2"},
			allConsFailedErr.Error(),
		},
		{
			"ok",
			[]string{"localhost"},
			"",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewPrometheusMP(logr.Discard(), &v1.PrometheusAutoscalerSpec{
				Address: tc.addrs,
			}, "test")
			if err != nil {
				if len(tc.expErr) == 0 {
					t.Fatalf("unexpected err: %s", err)
				}
				if !strings.Contains(err.Error(), tc.expErr) {
					t.Fatalf("expected to get err %q; got %q instead", tc.expErr, err)
				}
			} else {
				if len(tc.expErr) > 0 {
					t.Fatalf("expected to get err %q; got nil instead", tc.expErr)
				}
			}
		})
	}
}

func TestPrometheusMP_Update(t *testing.T) {
	testCases := []struct {
		name                    string
		addrs                   int
		queryType               string
		expectedProductionRate  int64
		expectedConsumptionRate int64
		expectedOffset          int64
		expectedLagByPartition  time.Duration
	}{
		{
			"vector",
			1,
			"vector",
			100,
			10,
			1e3,
			time.Second * 10,
		},
		{
			"vector 0 production",
			1,
			"vector",
			0,
			10,
			1e3,
			0,
		},
		{
			"vector multiple hosts",
			3,
			"vector",
			100,
			10,
			1e3,
			time.Second * 10,
		},
		{
			"matrix",
			1,
			"matrix",
			100,
			10,
			1e3,
			time.Second * 10,
		},
		{
			"matrix multiple hosts",
			21,
			"matrix",
			100,
			10,
			1e3,
			time.Second * 10,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var addrs []string
			var servers []*httptest.Server
			for i := 0; i < tc.addrs; i++ {
				srv := httptest.NewServer(http.HandlerFunc(fakePromHandler))
				servers = append(servers, srv)
				addrs = append(addrs, srv.URL)
			}
			defer func() {
				for i := range servers {
					servers[i].Close()
				}
			}()

			pm, err := NewPrometheusMP(logr.Discard(), &v1.PrometheusAutoscalerSpec{
				Address: addrs,
				Production: v1.ProductionQuerySpec{
					Query:          fmt.Sprintf("%s/%d", tc.queryType, tc.expectedProductionRate),
					PartitionLabel: "partition",
				},
				Consumption: v1.ConsumptionQuerySpec{
					Query:          fmt.Sprintf("%s/%d", tc.queryType, tc.expectedConsumptionRate),
					PartitionLabel: "partition",
				},
				Offset: v1.OffsetQuerySpec{
					Query:          fmt.Sprintf("%s/%d", tc.queryType, tc.expectedOffset),
					PartitionLabel: "partition",
				},
			}, "test")
			testCheckErr(t, err)

			testEqualInt64(t, pm.GetProductionRate(1), 0)
			testEqualInt64(t, pm.GetConsumptionRate(1), 0)
			testEqualInt64(t, pm.GetMessagesBehind(1), 0)

			err = pm.Update()
			testCheckErr(t, err)

			testEqualInt64(t, pm.GetProductionRate(1), tc.expectedProductionRate)
			testEqualInt64(t, pm.GetConsumptionRate(1), tc.expectedConsumptionRate)
			testEqualInt64(t, pm.GetMessagesBehind(1), tc.expectedOffset)
			testEqualInt64(t, int64(pm.GetLagByPartition(1)), int64(tc.expectedLagByPartition))
		})
	}

}

const vectorResponseFormat = `{
   "status": "success",
   "data": {
      "resultType": "vector",
      "result": [{
            "metric": {
               "__name__": "foobar",
               "partition": "1"
            },
            "value": [1435781451.781, "%s"]
         },
		 {
            "metric": {
               "__name__": "foobar",
               "partition": "2"
            },
            "value": [1435781451.781, "100"]
         }]
   }
}`

const matrixResponseFormat = `{
   "status" : "success",
   "data" : {
      "resultType" : "matrix",
      "result" : [
         {
            "metric" : {
               "__name__" : "foobar",
               "partition": "1"
            },
            "values" : [
               [ 1435781430.781, "1" ],
               [ 1435781445.781, "1" ],
               [ 1435781460.781, "%s" ]
            ]
         },
         {
            "metric" : {
               "__name__" : "foobar",
               "partition": "2"
            },
            "values" : [
               [ 1435781430.781, "0" ],
               [ 1435781445.781, "0" ],
               [ 1435781460.781, "1" ]
            ]
         }
      ]
   }
}`

func fakePromHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondWithErr(w, "non-GET request received")
		return
	}
	if r.URL.Path != "/api/v1/query" {
		respondWithErr(w, fmt.Sprintf("wrong path %q requested", r.URL.Path))
		return
	}
	b, err := io.ReadAll(r.Body)
	if err != nil {
		respondWithErr(w, fmt.Sprintf("failed to read body: %s", err))
		return
	}
	query, err := url.ParseQuery(string(b))
	if err != nil {
		respondWithErr(w, fmt.Sprintf("failed to parse query: %s", err))
		return
	}
	q := query.Get("query")
	if len(q) == 0 {
		respondWithErr(w, fmt.Sprintf("param 'query' can't be empty"))
		return
	}
	segments := strings.Split(q, "/")
	if len(segments) != 2 {
		respondWithErr(w, fmt.Sprintf("unsupported query %q", q))
	}

	t, val := segments[0], segments[1]
	switch t {
	case "vector":
		_, _ = fmt.Fprintf(w, fmt.Sprintf(vectorResponseFormat, val))
	case "matrix":
		_, _ = fmt.Fprintf(w, fmt.Sprintf(matrixResponseFormat, val))
	}
	w.Header().Set("Content-Type", "application/json")
}

func respondWithErr(w http.ResponseWriter, err string) {
	w.WriteHeader(http.StatusInternalServerError)
	fmt.Fprint(w, err)
}

func testCheckErr(t *testing.T, err error) {
	if err != nil {
		t.Fatalf("unexpected err: %s", err)
	}
}

func testEqualInt64(t *testing.T, a, b int64) {
	if a != b {
		t.Fatalf("expected %d to be equal to %d", a, b)
	}
}

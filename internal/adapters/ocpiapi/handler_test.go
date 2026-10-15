package ocpiapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"device-ingestion/internal/domain"
	"device-ingestion/internal/observability"
)

type testStatusReader struct {
	statuses map[string]map[int]domain.ConnectorStatus
	err      error
}

func (r testStatusReader) ConnectorStatuses(context.Context) (map[string]map[int]domain.ConnectorStatus, error) {
	return r.statuses, r.err
}

func TestLocationsRequiresPartnerTokenAndPartyHeaders(t *testing.T) {
	handler := NewHandler("NL", "ABC", "https://cpo.example", nil, []Partner{{CountryCode: "DE", PartyID: "XYZ", Token: "partner-secret"}}, testStatusReader{}, observability.NewMetrics())
	request := httptest.NewRequest(http.MethodGet, "/ocpi/cpo/2.2.1/locations", nil)
	request.Header.Set("X-Request-ID", "request-1")
	request.Header.Set("X-Correlation-ID", "correlation-1")
	request.Header.Set("Authorization", "Token wrong")
	request.Header.Set("OCPI-from-country-code", "DE")
	request.Header.Set("OCPI-from-party-id", "XYZ")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestLocationsReturnsLiveStatusAndEchoesRequestHeaders(t *testing.T) {
	updated := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	station := StationConfig{ChargingStation: domain.ChargingStation{
		ID: "station-1", LocationID: "location-1", Name: "Depot", Address: "1 Main", City: "Utrecht", Country: "NLD", TimeZone: "Europe/Amsterdam",
		Connectors: []domain.ChargingConnector{{ConnectorID: 1, EVSEUID: "evse-1", Standard: "IEC_62196_T2", Format: "SOCKET", PowerType: "AC_3_PHASE", MaxVoltage: 230, MaxAmperage: 32, MaxElectricPower: 22000}},
	}}
	handler := NewHandler("NL", "ABC", "https://cpo.example", []StationConfig{station}, []Partner{{CountryCode: "DE", PartyID: "XYZ", Token: "partner-secret"}}, testStatusReader{statuses: map[string]map[int]domain.ConnectorStatus{"station-1": {1: {Status: "Charging", StatusAt: updated}}}}, observability.NewMetrics())
	request := httptest.NewRequest(http.MethodGet, "/ocpi/cpo/2.2.1/locations/location-1/evse-1/1", nil)
	request.Header.Set("X-Request-ID", "request-1")
	request.Header.Set("X-Correlation-ID", "correlation-1")
	request.Header.Set("Authorization", "Token partner-secret")
	request.Header.Set("OCPI-from-country-code", "DE")
	request.Header.Set("OCPI-from-party-id", "XYZ")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("X-Request-ID") != "request-1" || response.Header().Get("X-Correlation-ID") != "correlation-1" {
		t.Fatalf("request headers were not echoed: %#v", response.Header())
	}
	var body envelope
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode OCPI response: %v", err)
	}
	var got connector
	encoded, _ := json.Marshal(body.Data)
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("decode connector: %v", err)
	}
	if got.ID != "1" || !got.LastUpdated.Equal(updated) {
		t.Fatalf("connector = %#v", got)
	}
}

func TestFilterLocationsRejectsInvalidPagination(t *testing.T) {
	if _, err := filterLocations(nil, mapValues("limit", "101")); err == nil {
		t.Fatal("filterLocations accepted a limit above 100")
	}
}

func TestVersionDiscoveryAndDetails(t *testing.T) {
	handler := NewHandler("NL", "ABC", "https://cpo.example/", nil, nil, testStatusReader{}, observability.NewMetrics())
	for path, want := range map[string]string{"/ocpi/versions": `"version":"2.2.1"`, "/ocpi/2.2.1": `"identifier":"locations"`} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), want) {
			t.Errorf("GET %s: status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestLocationsValidationPaginationAndStatusReaderErrors(t *testing.T) {
	station := StationConfig{ChargingStation: domain.ChargingStation{ID: "station-1", LocationID: "location-1", Connectors: []domain.ChargingConnector{{ConnectorID: 1, EVSEUID: "evse-1"}}}}
	partner := Partner{CountryCode: "DE", PartyID: "XYZ", Token: "partner-secret"}
	newRequest := func(query string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/ocpi/cpo/2.2.1/locations"+query, nil)
		r.Header.Set("X-Request-ID", "r")
		r.Header.Set("X-Correlation-ID", "c")
		r.Header.Set("Authorization", "Token partner-secret")
		r.Header.Set("OCPI-from-country-code", "DE")
		r.Header.Set("OCPI-from-party-id", "XYZ")
		return r
	}
	t.Run("requires request headers", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/ocpi/cpo/2.2.1/locations", nil)
		response := httptest.NewRecorder()
		NewHandler("NL", "ABC", "https://cpo.example", []StationConfig{station}, []Partner{partner}, testStatusReader{}, observability.NewMetrics()).ServeHTTP(response, r)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", response.Code)
		}
	})
	t.Run("bad limit", func(t *testing.T) {
		response := httptest.NewRecorder()
		NewHandler("NL", "ABC", "https://cpo.example", []StationConfig{station}, []Partner{partner}, testStatusReader{}, observability.NewMetrics()).ServeHTTP(response, newRequest("?limit=101"))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", response.Code)
		}
	})
	t.Run("status store failure", func(t *testing.T) {
		response := httptest.NewRecorder()
		NewHandler("NL", "ABC", "https://cpo.example", []StationConfig{station}, []Partner{partner}, testStatusReader{err: context.DeadlineExceeded}, observability.NewMetrics()).ServeHTTP(response, newRequest(""))
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", response.Code)
		}
	})
	t.Run("pagination headers", func(t *testing.T) {
		stations := []StationConfig{station, {ChargingStation: domain.ChargingStation{ID: "station-2", LocationID: "location-2"}}}
		response := httptest.NewRecorder()
		NewHandler("NL", "ABC", "https://cpo.example", stations, []Partner{partner}, testStatusReader{}, observability.NewMetrics()).ServeHTTP(response, newRequest("?limit=1"))
		if response.Header().Get("X-Total-Count") != "2" || response.Header().Get("Link") == "" {
			t.Fatalf("headers = %#v", response.Header())
		}
	})
}

func TestOCPIStatusAndPriorityMappings(t *testing.T) {
	for input, want := range map[string]string{"Available": "AVAILABLE", "Charging": "CHARGING", "Reserved": "RESERVED", "Unavailable": "INOPERATIVE", "Faulted": "OUTOFORDER", "bogus": "UNKNOWN"} {
		if got := ocpiStatus(input); got != want {
			t.Errorf("ocpiStatus(%q) = %q, want %q", input, got, want)
		}
	}
	for _, status := range []string{"AVAILABLE", "CHARGING", "RESERVED", "OUTOFORDER", "INOPERATIVE", "UNKNOWN"} {
		if statusPriority(status) == 0 {
			t.Errorf("statusPriority(%q) = 0", status)
		}
	}
	if statusPriority("AVAILABLE") <= statusPriority("CHARGING") || statusPriority("UNKNOWN") >= statusPriority("INOPERATIVE") {
		t.Fatal("status priority ordering is incorrect")
	}
}

func TestFilterLocationsByDatesAndRejectInvalidOffset(t *testing.T) {
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	locations := []location{{ID: "one", LastUpdated: stamp}, {ID: "two", LastUpdated: stamp.Add(time.Hour)}}
	filtered, err := filterLocations(locations, mapValues("date_from", stamp.Add(30*time.Minute).Format(time.RFC3339)))
	if err != nil || len(filtered) != 1 || filtered[0].ID != "two" {
		t.Fatalf("filtered=%v err=%v", filtered, err)
	}
	if _, err := filterLocations(locations, mapValues("offset", "-1")); err == nil {
		t.Fatal("negative offset accepted")
	}
	filtered, err = filterLocations(locations, mapValues("date_to", stamp.Format(time.RFC3339)))
	if err != nil || len(filtered) != 1 || filtered[0].ID != "one" {
		t.Fatalf("date_to filter = %v, err = %v", filtered, err)
	}
	if _, err := filterLocations(locations, mapValues("date_to", "invalid")); err == nil {
		t.Fatal("invalid date_to accepted")
	}
}

func TestLocationsUnknownPathsAndWrongMethod(t *testing.T) {
	handler := NewHandler("NL", "ABC", "https://cpo.example", nil, nil, testStatusReader{}, observability.NewMetrics())
	for _, tc := range []struct {
		method, path string
		status       int
	}{{http.MethodPost, "/ocpi/versions", http.StatusNotFound}, {http.MethodPost, "/ocpi/2.2.1", http.StatusNotFound}, {http.MethodGet, "/missing", http.StatusNotFound}, {http.MethodPost, "/ocpi/cpo/2.2.1/locations", http.StatusMethodNotAllowed}} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
		if response.Code != tc.status {
			t.Errorf("%s %s status=%d want=%d", tc.method, tc.path, response.Code, tc.status)
		}
	}
}

func TestLocationsRejectsPartnerAndQueryMismatches(t *testing.T) {
	partner := Partner{CountryCode: "DE", PartyID: "XYZ", Token: "partner-secret"}
	newRequest := func(query string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/ocpi/cpo/2.2.1/locations"+query, nil)
		r.Header.Set("X-Request-ID", "r")
		r.Header.Set("X-Correlation-ID", "c")
		r.Header.Set("Authorization", "Token partner-secret")
		r.Header.Set("OCPI-from-country-code", "DE")
		r.Header.Set("OCPI-from-party-id", "XYZ")
		return r
	}
	for _, tc := range []struct {
		name   string
		mutate func(*http.Request)
		query  string
	}{
		{"wrong recipient", func(r *http.Request) { r.Header.Set("OCPI-to-party-id", "ZZZ") }, ""},
		{"wrong origin", func(r *http.Request) { r.Header.Set("OCPI-from-party-id", "NOPE") }, ""},
		{"malformed date", nil, "?date_from=not-a-date"},
		{"invalid offset", nil, "?offset=-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRequest(tc.query)
			if tc.mutate != nil {
				tc.mutate(r)
			}
			response := httptest.NewRecorder()
			NewHandler("NL", "ABC", "https://cpo.example", nil, []Partner{partner}, testStatusReader{}, observability.NewMetrics()).ServeHTTP(response, r)
			want := http.StatusBadRequest
			if tc.name == "wrong recipient" || tc.name == "wrong origin" {
				want = http.StatusUnauthorized
			}
			if response.Code != want {
				t.Fatalf("status=%d want=%d", response.Code, want)
			}
		})
	}
}

func TestLocationsNestedResourceNotFound(t *testing.T) {
	station := StationConfig{ChargingStation: domain.ChargingStation{ID: "station-1", LocationID: "location-1", Connectors: []domain.ChargingConnector{{ConnectorID: 1, EVSEUID: "evse-1"}}}}
	handler := NewHandler("NL", "ABC", "https://cpo.example", []StationConfig{station}, []Partner{{CountryCode: "DE", PartyID: "XYZ", Token: "partner-secret"}}, testStatusReader{}, observability.NewMetrics())
	request := httptest.NewRequest(http.MethodGet, "/ocpi/cpo/2.2.1/locations/location-1/evse-missing", nil)
	request.Header.Set("X-Request-ID", "r")
	request.Header.Set("X-Correlation-ID", "c")
	request.Header.Set("Authorization", "Token partner-secret")
	request.Header.Set("OCPI-from-country-code", "DE")
	request.Header.Set("OCPI-from-party-id", "XYZ")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d", response.Code)
	}
}

func mapValues(key, value string) map[string][]string {
	return map[string][]string{key: {value}}
}

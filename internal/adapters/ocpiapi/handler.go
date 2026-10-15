package ocpiapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"device-ingestion/internal/domain"
	"device-ingestion/internal/observability"
)

type StationConfig struct {
	domain.ChargingStation
}

type Partner struct {
	CountryCode string
	PartyID     string
	Token       string
}

type StatusReader interface {
	ConnectorStatuses(context.Context) (map[string]map[int]domain.ConnectorStatus, error)
}

type Handler struct {
	countryCode string
	partyID     string
	baseURL     string
	stations    []StationConfig
	partners    []Partner
	status      StatusReader
	metrics     *observability.Metrics
	startedAt   time.Time
}

func NewHandler(countryCode, partyID, baseURL string, stations []StationConfig, partners []Partner, status StatusReader, metrics *observability.Metrics) *Handler {
	return &Handler{countryCode: countryCode, partyID: partyID, baseURL: strings.TrimRight(baseURL, "/"), stations: stations, partners: partners, status: status, metrics: metrics, startedAt: time.Now().UTC()}
}

type envelope struct {
	StatusCode    int         `json:"status_code"`
	StatusMessage *string     `json:"status_message"`
	Timestamp     time.Time   `json:"timestamp"`
	Data          interface{} `json:"data"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	result, endpoint := "failed", "other"
	defer func() {
		h.metrics.AddCounter("device_ingestion_ocpi_requests_total", map[string]string{"version": "2.2.1", "endpoint": endpoint, "result": result}, 1)
		h.metrics.Observe("device_ingestion_ocpi_request_duration_seconds", map[string]string{"endpoint": endpoint}, time.Since(started))
	}()
	path := strings.TrimSuffix(r.URL.Path, "/")
	switch path {
	case "/ocpi/versions":
		endpoint = "versions"
		if r.Method != http.MethodGet {
			result = "method_not_allowed"
			h.notFound(w, r)
			return
		}
		result = "success"
		h.write(w, r, http.StatusOK, []any{map[string]string{"version": "2.2.1", "url": h.baseURL + "/ocpi/2.2.1"}})
		return
	case "/ocpi/2.2.1":
		endpoint = "version_details"
		if r.Method != http.MethodGet {
			result = "method_not_allowed"
			h.notFound(w, r)
			return
		}
		result = "success"
		h.write(w, r, http.StatusOK, map[string]any{
			"version": "2.2.1",
			"endpoints": []any{map[string]any{
				"identifier": "locations", "role": "SENDER", "url": h.baseURL + "/ocpi/cpo/2.2.1/locations",
			}},
		})
		return
	}
	const locationsPath = "/ocpi/cpo/2.2.1/locations"
	if path != locationsPath && !strings.HasPrefix(path, locationsPath+"/") {
		endpoint = "other"
		result = "not_found"
		h.notFound(w, r)
		return
	}
	endpoint = "locations"
	if r.Method != http.MethodGet {
		result = "method_not_allowed"
		h.ocpiError(w, r, http.StatusMethodNotAllowed, 2001, "Method not allowed")
		return
	}
	if !h.requestHeadersValid(r) {
		result = "bad_request"
		h.ocpiError(w, r, http.StatusBadRequest, 2001, "X-Request-ID and X-Correlation-ID are required")
		return
	}
	if !h.authorized(r) {
		result = "unauthorized"
		h.ocpiError(w, r, http.StatusUnauthorized, 2002, "Invalid credentials or party")
		return
	}
	resourcePath := strings.Trim(strings.TrimPrefix(path, locationsPath), "/")
	locations, err := h.locations(r.Context())
	if err != nil {
		result = "unavailable"
		h.ocpiError(w, r, http.StatusInternalServerError, 3000, "Unable to retrieve locations")
		return
	}
	locations, err = filterLocations(locations, r.URL.Query())
	if err != nil {
		result = "bad_request"
		h.ocpiError(w, r, http.StatusBadRequest, 2001, "Invalid pagination or timestamp query")
		return
	}
	if resourcePath != "" {
		parts := strings.Split(resourcePath, "/")
		var found *location
		for i := range locations {
			if locations[i].ID == parts[0] {
				found = &locations[i]
				break
			}
		}
		if found == nil {
			result = "not_found"
			h.notFound(w, r)
			return
		}
		if len(parts) == 1 {
			result = "success"
			h.write(w, r, http.StatusOK, found)
			return
		}
		if len(parts) == 2 {
			for _, item := range found.EVSEs {
				if item.UID == parts[1] {
					result = "success"
					h.write(w, r, http.StatusOK, item)
					return
				}
			}
		}
		if len(parts) == 3 {
			for _, item := range found.EVSEs {
				if item.UID != parts[1] {
					continue
				}
				for _, connected := range item.Connectors {
					if connected.ID == parts[2] {
						result = "success"
						h.write(w, r, http.StatusOK, connected)
						return
					}
				}
			}
		}
		result = "not_found"
		h.notFound(w, r)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	total := len(locations)
	if offset > total {
		offset = total
	}
	end := min(offset+limit, total)
	page := locations[offset:end]
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	w.Header().Set("X-Limit", strconv.Itoa(limit))
	if end < total {
		next := *r.URL
		query := next.Query()
		query.Set("offset", strconv.Itoa(end))
		query.Set("limit", strconv.Itoa(limit))
		next.RawQuery = query.Encode()
		w.Header().Add("Link", "<"+next.String()+">; rel=\"next\"")
	}
	result = "success"
	h.write(w, r, http.StatusOK, page)
}

func (h *Handler) locations(ctx context.Context) ([]location, error) {
	statuses, err := h.status.ConnectorStatuses(ctx)
	if err != nil {
		return nil, err
	}
	byLocation := make(map[string]*location)
	stationByLocation := make(map[string]map[string]struct{})
	now := h.startedAt
	for _, station := range h.stations {
		loc := byLocation[station.LocationID]
		if loc == nil {
			loc = &location{CountryCode: h.countryCode, PartyID: h.partyID, ID: station.LocationID, Publish: true, Name: station.Name, Address: station.Address, City: station.City, PostalCode: station.PostalCode, Country: strings.ToUpper(station.Country), Coordinates: coordinates{Latitude: fmt.Sprintf("%.6f", station.Latitude), Longitude: fmt.Sprintf("%.6f", station.Longitude)}, TimeZone: station.TimeZone, LastUpdated: now}
			byLocation[station.LocationID] = loc
			stationByLocation[station.LocationID] = make(map[string]struct{})
		}
		if _, exists := stationByLocation[station.LocationID][station.ID]; exists {
			continue
		}
		stationByLocation[station.LocationID][station.ID] = struct{}{}
		evseByUID := make(map[string]*evse)
		for _, profile := range station.Connectors {
			item := evseByUID[profile.EVSEUID]
			if item == nil {
				item = &evse{UID: profile.EVSEUID, Status: "UNKNOWN", Connectors: make([]connector, 0), LastUpdated: now}
				evseByUID[profile.EVSEUID] = item
			}
			current, ok := statuses[station.ID][profile.ConnectorID]
			connectorStatus, updated := "UNKNOWN", now
			if ok {
				connectorStatus, updated = ocpiStatus(current.Status), current.StatusAt
				if updated.IsZero() {
					updated = now
				}
			}
			if updated.After(item.LastUpdated) || item.LastUpdated.Equal(now) {
				item.LastUpdated = updated
			}
			if item.Status == "UNKNOWN" || statusPriority(connectorStatus) > statusPriority(item.Status) {
				item.Status = connectorStatus
			}
			item.Connectors = append(item.Connectors, connector{ID: strconv.Itoa(profile.ConnectorID), Standard: profile.Standard, Format: profile.Format, PowerType: profile.PowerType, MaxVoltage: profile.MaxVoltage, MaxAmperage: profile.MaxAmperage, MaxElectricPower: profile.MaxElectricPower, LastUpdated: updated})
		}
		for _, item := range evseByUID {
			if len(item.Connectors) == 0 {
				continue
			}
			loc.EVSEs = append(loc.EVSEs, *item)
			if item.LastUpdated.After(loc.LastUpdated) || loc.LastUpdated.Equal(now) {
				loc.LastUpdated = item.LastUpdated
			}
		}
	}
	result := make([]location, 0, len(byLocation))
	for _, loc := range byLocation {
		sort.Slice(loc.EVSEs, func(i, j int) bool { return loc.EVSEs[i].UID < loc.EVSEs[j].UID })
		result = append(result, *loc)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func ocpiStatus(status string) string {
	switch status {
	case "Available":
		return "AVAILABLE"
	case "Charging", "Preparing", "SuspendedEV", "SuspendedEVSE", "Finishing":
		return "CHARGING"
	case "Reserved":
		return "RESERVED"
	case "Unavailable":
		return "INOPERATIVE"
	case "Faulted":
		return "OUTOFORDER"
	default:
		return "UNKNOWN"
	}
}

func statusPriority(status string) int {
	switch status {
	case "AVAILABLE":
		return 6
	case "CHARGING":
		return 5
	case "RESERVED":
		return 4
	case "OUTOFORDER":
		return 3
	case "INOPERATIVE":
		return 2
	default:
		return 1
	}
}

func filterLocations(locations []location, query url.Values) ([]location, error) {
	var from, to time.Time
	var err error
	if value := query.Get("date_from"); value != "" {
		from, err = time.Parse(time.RFC3339, value)
		if err != nil {
			return nil, err
		}
	}
	if value := query.Get("date_to"); value != "" {
		to, err = time.Parse(time.RFC3339, value)
		if err != nil {
			return nil, err
		}
	}
	offset := 0
	limit := 50
	if value := query.Get("offset"); value != "" {
		offset, err = strconv.Atoi(value)
		if err != nil || offset < 0 {
			return nil, fmt.Errorf("invalid offset")
		}
	}
	if value := query.Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			return nil, fmt.Errorf("invalid limit")
		}
	}
	_ = offset
	_ = limit
	filtered := make([]location, 0, len(locations))
	for _, item := range locations {
		if !from.IsZero() && item.LastUpdated.Before(from) {
			continue
		}
		if !to.IsZero() && item.LastUpdated.After(to) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered, nil
}

func (h *Handler) authorized(r *http.Request) bool {
	const prefix = "Token "
	authorization := r.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, prefix) {
		return false
	}
	provided := []byte(strings.TrimPrefix(authorization, prefix))
	fromCountry := strings.ToUpper(r.Header.Get("OCPI-from-country-code"))
	fromParty := strings.ToUpper(r.Header.Get("OCPI-from-party-id"))
	toCountry := strings.ToUpper(r.Header.Get("OCPI-to-country-code"))
	toParty := strings.ToUpper(r.Header.Get("OCPI-to-party-id"))
	if (toCountry != "" && toCountry != h.countryCode) || (toParty != "" && toParty != h.partyID) {
		return false
	}
	for _, partner := range h.partners {
		partyMatches := fromCountry == partner.CountryCode && fromParty == partner.PartyID
		if partyMatches && subtle.ConstantTimeCompare(provided, []byte(partner.Token)) == 1 {
			return true
		}
	}
	return false
}

func (h *Handler) requestHeadersValid(r *http.Request) bool {
	return r.Header.Get("X-Request-ID") != "" && r.Header.Get("X-Correlation-ID") != ""
}

func (h *Handler) write(w http.ResponseWriter, r *http.Request, status int, data any) {
	copyRequestHeaders(w, r)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{StatusCode: 1000, Timestamp: time.Now().UTC(), Data: data})
}

func (h *Handler) ocpiError(w http.ResponseWriter, r *http.Request, status, code int, message string) {
	copyRequestHeaders(w, r)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{StatusCode: code, StatusMessage: &message, Timestamp: time.Now().UTC(), Data: nil})
}

func (h *Handler) notFound(w http.ResponseWriter, r *http.Request) {
	h.ocpiError(w, r, http.StatusNotFound, 2003, "Resource not found")
}

func copyRequestHeaders(w http.ResponseWriter, r *http.Request) {
	for _, name := range []string{"X-Request-ID", "X-Correlation-ID"} {
		if value := r.Header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
}

type coordinates struct {
	Latitude  string `json:"latitude"`
	Longitude string `json:"longitude"`
}

type location struct {
	CountryCode string      `json:"country_code"`
	PartyID     string      `json:"party_id"`
	ID          string      `json:"id"`
	Publish     bool        `json:"publish"`
	Name        string      `json:"name"`
	Address     string      `json:"address"`
	City        string      `json:"city"`
	PostalCode  string      `json:"postal_code"`
	Country     string      `json:"country"`
	Coordinates coordinates `json:"coordinates"`
	EVSEs       []evse      `json:"evses"`
	TimeZone    string      `json:"time_zone"`
	LastUpdated time.Time   `json:"last_updated"`
}

type evse struct {
	UID         string      `json:"uid"`
	Status      string      `json:"status"`
	Connectors  []connector `json:"connectors"`
	LastUpdated time.Time   `json:"last_updated"`
}

type connector struct {
	ID               string    `json:"id"`
	Standard         string    `json:"standard"`
	Format           string    `json:"format"`
	PowerType        string    `json:"power_type"`
	MaxVoltage       int       `json:"max_voltage"`
	MaxAmperage      int       `json:"max_amperage"`
	MaxElectricPower int       `json:"max_electric_power"`
	LastUpdated      time.Time `json:"last_updated"`
}

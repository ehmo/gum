package main

import "github.com/ehmo/gum/internal/catalog"

// makeAPIKeyOp builds a read-class REST op authenticated with an API key
// (auth_strategy=api_key, no OAuth scopes). The key is resolved from the
// keyring / GUM_API_KEY and sent as both the X-Goog-Api-Key header and the
// ?key= query param by the typed-rest-sdk adapter. fields may be nil for ops
// whose RequestFields are Discovery-enriched (e.g. Custom Search).
func makeAPIKeyOp(opID, variantID, title, summary, service string, risk catalog.RiskClass, method, path, goPkg, goCall string, fields []catalog.RequestField, headerParams map[string]string) catalog.Op {
	return catalog.Op{
		OpID:             opID,
		OpSchemaVersion:  1,
		Title:            title,
		Summary:          summary,
		Service:          service,
		ServiceFamily:    "workspace",
		DefaultVariantID: variantID,
		RequestFields:    fields,
		Variants: []catalog.Variant{
			{
				VariantID:            variantID,
				VariantSchemaVersion: 1,
				Version:              versionFromVariantID(variantID),
				Stability:            catalog.StabilityStable,
				InterfaceKind:        catalog.InterfaceKindDiscoveryREST,
				BackendKind:          catalog.BackendKindTypedRestSDK,
				Preferred:            true,
				RiskClass:            risk,
				AuthStrategy:         catalog.AuthStrategyAPIKey,
				Binding: &catalog.Binding{
					BindingSchemaVersion: 1,
					AdapterKey:           "rest.typed-rest-sdk",
					OperationKey:         opID,
					HTTP:                 &catalog.HTTPBinding{Method: method, Path: path, HeaderParams: headerParams},
					GoPkg:                goPkg,
					GoCall:               goCall,
				},
			},
		},
	}
}

func qf(name, typ, desc string, required bool) catalog.RequestField {
	return catalog.RequestField{Name: name, Location: catalog.RequestFieldQuery, Type: typ, Required: required, Description: desc}
}

// hf declares a header-routed request field (paired with a Binding.HeaderParams
// entry that names the actual HTTP header).
func hf(name, desc string, required bool) catalog.RequestField {
	return catalog.RequestField{Name: name, Location: catalog.RequestFieldHeader, Type: "string", Required: required, Description: desc}
}

// BuildCustomSearchOps returns the Custom Search JSON API (customsearch/v1):
// programmable web/image search. auth_strategy=api_key; the caller supplies the
// search-engine id `cx`. RequestFields are Discovery-enriched (the method id is
// search.cse.list — see discoveryMethodID).
func BuildCustomSearchOps() []catalog.Op {
	return []catalog.Op{
		makeAPIKeyOp(
			"customsearch.cse.list", "customsearch.v1.rest.cse.list", "Programmable Web Search",
			"Run a Custom Search query (q) against a programmable search engine (cx). Returns web results; supports num, start, searchType=image, siteSearch, etc.",
			"customsearch", catalog.RiskClassRead, "GET",
			"https://customsearch.googleapis.com/customsearch/v1",
			"google.golang.org/api/customsearch/v1", "Cse.List", nil, nil),
	}
}

// BuildMapsOps returns a curated Google Maps Platform surface using the classic
// web services (api_key auth). RequestFields are hand-authored (these endpoints
// have no standard Discovery doc). The modern Places (New) + Routes APIs live in
// BuildPlacesRoutesOps (they need the X-Goog-FieldMask header).
func BuildMapsOps() []catalog.Op {
	const goPkg = "" // classic web services have no typed Go SDK package
	return []catalog.Op{
		makeAPIKeyOp(
			"maps.geocoding.geocode", "maps.v1.rest.geocoding.geocode", "Geocode an Address",
			"Convert an address to coordinates (address=) or coordinates to an address (latlng=). Optional: components, region, language.",
			"maps", catalog.RiskClassRead, "GET",
			"https://maps.googleapis.com/maps/api/geocode/json", goPkg, "Geocoding.Geocode",
			[]catalog.RequestField{
				qf("address", "string", "The street address or plus code to geocode.", false),
				qf("latlng", "string", "Latitude,longitude for reverse geocoding (e.g. 40.714,-73.961).", false),
				qf("place_id", "string", "A place ID to geocode.", false),
				qf("components", "string", "Component filter (e.g. country:US|postal_code:94043).", false),
				qf("region", "string", "Region bias ccTLD (e.g. us).", false),
				qf("language", "string", "Language for results (e.g. en).", false),
			}, nil),
		makeAPIKeyOp(
			"maps.timezone.get", "maps.v1.rest.timezone.get", "Get Time Zone",
			"Return the time zone for a location at a given time (location=lat,lng; timestamp=Unix seconds).",
			"maps", catalog.RiskClassRead, "GET",
			"https://maps.googleapis.com/maps/api/timezone/json", goPkg, "TimeZone.Get",
			[]catalog.RequestField{
				qf("location", "string", "Latitude,longitude (e.g. 40.714,-73.961).", true),
				qf("timestamp", "integer", "Unix timestamp (seconds) the offset is computed for.", true),
				qf("language", "string", "Language for the time-zone name.", false),
			}, nil),
		makeAPIKeyOp(
			"maps.distancematrix.get", "maps.v1.rest.distancematrix.get", "Distance Matrix",
			"Travel distance and time for a matrix of origins and destinations.",
			"maps", catalog.RiskClassRead, "GET",
			"https://maps.googleapis.com/maps/api/distancematrix/json", goPkg, "DistanceMatrix.Get",
			[]catalog.RequestField{
				qf("origins", "string", "Pipe-separated origin places/coords (e.g. Boston,MA|New York,NY).", true),
				qf("destinations", "string", "Pipe-separated destination places/coords.", true),
				qf("mode", "string", "Travel mode: driving|walking|bicycling|transit.", false),
				qf("units", "string", "Unit system: metric|imperial.", false),
				qf("departure_time", "string", "Departure time (Unix seconds or 'now') for traffic-aware durations.", false),
				qf("language", "string", "Language for results.", false),
			}, nil),
	}
}

// BuildPlacesRoutesOps returns the modern Maps APIs that require a per-request
// X-Goog-FieldMask header (which fields to return) routed via Binding.HeaderParams:
//   - Places API (New): text + nearby search (POST, body holds the query)
//   - Routes API: computeRoutes + computeRouteMatrix (POST, body holds origins/dest)
//
// auth_strategy=api_key. The caller passes fieldMask (e.g.
// "routes.duration,routes.distanceMeters" or "places.displayName,places.formattedAddress")
// and the request payload via body:=.
func BuildPlacesRoutesOps() []catalog.Op {
	fm := map[string]string{"fieldMask": "X-Goog-FieldMask"}
	maskField := func(example string) []catalog.RequestField {
		return []catalog.RequestField{hf("fieldMask", "Required X-Goog-FieldMask — comma-separated response fields (e.g. "+example+", or * for all, which bills at the highest tier).", true)}
	}
	return []catalog.Op{
		makeAPIKeyOp(
			"places.searchText", "places.v1.rest.places.searchText", "Search Places by Text",
			"Places API (New) text search (args.body.textQuery, e.g. \"pizza in NYC\"). Requires fieldMask.",
			"places", catalog.RiskClassRead, "POST",
			"https://places.googleapis.com/v1/places:searchText", "", "Places.SearchText",
			maskField("places.displayName,places.formattedAddress,places.location"), fm),
		makeAPIKeyOp(
			"places.searchNearby", "places.v1.rest.places.searchNearby", "Search Nearby Places",
			"Places API (New) nearby search (args.body.locationRestriction.circle + includedTypes). Requires fieldMask.",
			"places", catalog.RiskClassRead, "POST",
			"https://places.googleapis.com/v1/places:searchNearby", "", "Places.SearchNearby",
			maskField("places.displayName,places.location"), fm),
		makeAPIKeyOp(
			"routes.computeRoutes", "routes.v2.rest.routes.computeRoutes", "Compute Routes",
			"Routes API directions between an origin and destination (args.body: origin, destination, travelMode). Requires fieldMask.",
			"routes", catalog.RiskClassRead, "POST",
			"https://routes.googleapis.com/directions/v2:computeRoutes", "", "Routes.ComputeRoutes",
			maskField("routes.duration,routes.distanceMeters,routes.polyline.encodedPolyline"), fm),
		makeAPIKeyOp(
			"routes.computeRouteMatrix", "routes.v2.rest.routes.computeRouteMatrix", "Compute Route Matrix",
			"Routes API distance/time matrix (args.body: origins[], destinations[], travelMode). Requires fieldMask.",
			"routes", catalog.RiskClassRead, "POST",
			"https://routes.googleapis.com/distanceMatrix/v2:computeRouteMatrix", "", "Routes.ComputeRouteMatrix",
			maskField("originIndex,destinationIndex,duration,distanceMeters"), fm),
	}
}

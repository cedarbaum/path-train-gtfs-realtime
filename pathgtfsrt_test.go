package pathgtfsrt

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/google/go-cmp/cmp"
	"github.com/jamespfennell/path-train-gtfs-realtime/proto/gtfsrt"
	gtfs "github.com/jamespfennell/path-train-gtfs-realtime/proto/gtfsrt"
	portauthorityv1 "github.com/jamespfennell/path-train-gtfs-realtime/proto/portauthority"
	sourceapi "github.com/jamespfennell/path-train-gtfs-realtime/proto/sourceapi"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	stopID14St    = "stopID1"
	stopIDHoboken = "stopID2"
	routeID1      = "routeID1"
	routeID2      = "routeID2"
	agencyId      = "151"
)

func TestTripUpdateFeed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		updates []update
	}{
		{
			name: "missing route",
			updates: []update{
				{
					data: map[sourceapi.Station][]Train{
						sourceapi.Station_HOBOKEN: {
							{
								Direction:        sourceapi.Direction_TO_NJ,
								ProjectedArrival: makeTimestamppb(5),
								LastUpdated:      makeTimestamppb(10),
							},
						},
						sourceapi.Station_FOURTEENTH_STREET: {},
					},
					wantErrs:         0,
					wantFeedEntities: nil,
				},
			},
		},
		{
			name: "missing direction",
			updates: []update{
				{
					data: map[sourceapi.Station][]Train{
						sourceapi.Station_HOBOKEN: {
							{
								Route:            sourceapi.Route_HOB_33,
								ProjectedArrival: makeTimestamppb(5),
								LastUpdated:      makeTimestamppb(10),
							},
						},
						sourceapi.Station_FOURTEENTH_STREET: {},
					},
					wantErrs:         0,
					wantFeedEntities: nil,
				},
			},
		},
		{
			name: "missing arrival",
			updates: []update{
				{
					data: map[sourceapi.Station][]Train{
						sourceapi.Station_HOBOKEN: {
							{
								Route:       sourceapi.Route_HOB_33,
								Direction:   sourceapi.Direction_TO_NJ,
								LastUpdated: makeTimestamppb(10),
							},
						},
						sourceapi.Station_FOURTEENTH_STREET: {},
					},
					wantErrs:         0,
					wantFeedEntities: nil,
				},
			},
		},
		{
			name: "missing last updated",
			updates: []update{
				{
					data: map[sourceapi.Station][]Train{
						sourceapi.Station_HOBOKEN: {
							{
								Route:            sourceapi.Route_HOB_33,
								Direction:        sourceapi.Direction_TO_NJ,
								ProjectedArrival: makeTimestamppb(5),
							},
						},
						sourceapi.Station_FOURTEENTH_STREET: {},
					},
					wantErrs:         0,
					wantFeedEntities: nil,
				},
			},
		},
		{
			name: "regular update at two stops",
			updates: []update{
				{
					data: map[sourceapi.Station][]Train{
						sourceapi.Station_HOBOKEN: {
							sourceTrain(sourceapi.Route_HOB_33, sourceapi.Direction_TO_NY, 15, 10),
						},
						sourceapi.Station_FOURTEENTH_STREET: {
							sourceTrain(sourceapi.Route_HOB_33, sourceapi.Direction_TO_NJ, 20, 5),
						},
					},
					wantErrs: 0,
					wantFeedEntities: []*gtfsrt.FeedEntity{
						wantFeedEntity(routeID1, 1, stopIDHoboken, 15, 10),
						wantFeedEntity(routeID1, 0, stopID14St, 20, 5),
					},
				},
			},
		},
		{
			name: "regular update, two trains at one stop",
			updates: []update{
				{
					data: map[sourceapi.Station][]Train{
						sourceapi.Station_HOBOKEN: {
							sourceTrain(sourceapi.Route_HOB_33, sourceapi.Direction_TO_NY, 15, 10),
							sourceTrain(sourceapi.Route_HOB_33, sourceapi.Direction_TO_NJ, 20, 5),
						},
						sourceapi.Station_FOURTEENTH_STREET: {},
					},
					wantErrs: 0,
					wantFeedEntities: []*gtfsrt.FeedEntity{
						wantFeedEntity(routeID1, 1, stopIDHoboken, 15, 10),
						wantFeedEntity(routeID1, 0, stopIDHoboken, 20, 5),
					},
				},
			},
		},
		{
			name: "for request errors, keep old data",
			updates: []update{
				{
					data: map[sourceapi.Station][]Train{
						sourceapi.Station_HOBOKEN: {
							sourceTrain(sourceapi.Route_HOB_33, sourceapi.Direction_TO_NY, 15, 10),
						},
						sourceapi.Station_FOURTEENTH_STREET: {},
					},
					wantErrs: 0,
					wantFeedEntities: []*gtfsrt.FeedEntity{
						wantFeedEntity(routeID1, 1, stopIDHoboken, 15, 10),
					},
				},
				{
					data: map[sourceapi.Station][]Train{
						sourceapi.Station_FOURTEENTH_STREET: {},
					},
					wantErrs: 1,
					wantFeedEntities: []*gtfsrt.FeedEntity{
						wantFeedEntity(routeID1, 1, stopIDHoboken, 15, 10),
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := mockSourceClient{
				stationToStopID: map[sourceapi.Station]string{
					sourceapi.Station_FOURTEENTH_STREET: stopID14St,
					sourceapi.Station_HOBOKEN:           stopIDHoboken,
				},
				routeToRouteID: map[sourceapi.Route]string{
					sourceapi.Route_HOB_33: routeID1,
				},
				stationToTrains: map[sourceapi.Station][]Train{
					sourceapi.Station_FOURTEENTH_STREET: nil,
					sourceapi.Station_HOBOKEN:           nil,
				},
			}
			ctx := context.Background()
			updateSignal := make(chan []error, 1)

			c := clock.NewMock()
			staticData, err := GetStaticData(ctx, &client)
			feed, err := NewTripUpdateFeed(ctx, c, 5*time.Second, &client, staticData, func(msg *gtfsrt.FeedMessage, requestErrs []error) {
				updateSignal <- requestErrs
			})
			if err != nil {
				t.Fatalf("NewFeed() err got=%v, want=<nil>", err)
			}
			requestErrs := <-updateSignal
			if numErrs := len(requestErrs); numErrs != 0 {
				t.Errorf("callback errs got=%d, want=0", numErrs)
			}

			for _, update := range tc.updates {
				client.stationToTrains = update.data
				c.Add(5 * time.Second)
				requestErrs = <-updateSignal
				if numErrs := len(requestErrs); numErrs != update.wantErrs {
					t.Errorf("callback errs got=%d, want=%d", numErrs, update.wantErrs)
				}
				b := feed.Get()
				var gotMsg gtfsrt.FeedMessage
				if err := proto.Unmarshal(b, &gotMsg); err != nil {
					t.Errorf("proto.Unmarshal() errs got=%v, want=<nil>", err)
				}
				now := uint64(c.Now().Unix())
				wantMsg := gtfsrt.FeedMessage{
					Header: &gtfsrt.FeedHeader{
						GtfsRealtimeVersion: ptr("0.2"),
						Incrementality:      gtfsrt.FeedHeader_FULL_DATASET.Enum(),
						Timestamp:           &now,
					},
					Entity: update.wantFeedEntities,
				}
				if diff := cmp.Diff(&gotMsg, &wantMsg,
					protocmp.Transform(),
					protocmp.IgnoreFields(&gtfsrt.FeedEntity{}, "id"),
					protocmp.IgnoreFields(&gtfsrt.TripDescriptor{}, "trip_id"),
				); diff != "" {
					t.Errorf("GTFS realtime feed got != want, diff=%s", diff)
				}
			}
		})
	}
}

func TestPortAuthorityAlertFeed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		updates []alertUpdate
	}{
		{
			name: "simple alert",
			updates: []alertUpdate{
				{
					incidents: []Incident{
						{
							IncidentMessage: &portauthorityv1.GetIncidentsResponse_Incidentmessage{
								Subject:    "Alert header",
								PreMessage: "Alert description",
							},
						},
					},
					wantErrs: 0,
					wantFeedEntities: []*gtfsrt.FeedEntity{
						wantAlertFeedEntity(
							ptr("Alert header"),
							ptr("Alert description"),
							ptr(agencyId),
							nil,
							nil,
							nil),
					},
				},
			},
		},
		{
			name: "multiple incidents",
			updates: []alertUpdate{
				{
					incidents: []Incident{
						{
							IncidentMessage: &portauthorityv1.GetIncidentsResponse_Incidentmessage{
								Subject:    "Alert header 1",
								PreMessage: "Alert description 1",
							},
						},
						{
							IncidentMessage: &portauthorityv1.GetIncidentsResponse_Incidentmessage{
								Subject:    "Alert header 2",
								PreMessage: "Alert description 2",
							},
						},
					},
					wantErrs: 0,
					wantFeedEntities: []*gtfsrt.FeedEntity{
						wantAlertFeedEntity(
							ptr("Alert header 1"),
							ptr("Alert description 1"),
							ptr(agencyId),
							nil,
							nil,
							nil),
						wantAlertFeedEntity(
							ptr("Alert header 2"),
							ptr("Alert description 2"),
							ptr(agencyId),
							nil,
							nil,
							nil),
					},
				},
			},
		},
		{
			name: "no incidents",
			updates: []alertUpdate{
				{
					incidents:        nil,
					wantErrs:         0,
					wantFeedEntities: []*gtfsrt.FeedEntity{},
				},
			},
		},
		{
			name: "alert with status",
			updates: []alertUpdate{
				{
					incidents: []Incident{
						{
							IncidentMessage: &portauthorityv1.GetIncidentsResponse_Incidentmessage{
								Subject:    "Alert header",
								PreMessage: "Alert description",
								FormVariableItems: []*portauthorityv1.GetIncidentsResponse_Formvariableitems{
									{
										VariableName: "Status",
										Val:          []string{"delayed."},
									},
								},
							},
						},
					},
					wantErrs: 0,
					wantFeedEntities: []*gtfsrt.FeedEntity{
						wantAlertFeedEntity(
							ptr("Alert header"),
							ptr("Alert description"),
							ptr(agencyId),
							nil,
							nil,
							ptr(gtfs.Alert_SIGNIFICANT_DELAYS)),
					},
				},
			},
		},
		{
			name: "alert with station",
			updates: []alertUpdate{
				{
					incidents: []Incident{
						{
							IncidentMessage: &portauthorityv1.GetIncidentsResponse_Incidentmessage{
								Subject:    "Alert header",
								PreMessage: "Alert description",
								FormVariableItems: []*portauthorityv1.GetIncidentsResponse_Formvariableitems{
									{
										VariableName: "Station",
										Val:          []string{"14S"},
									},
								},
							},
						},
					},
					wantErrs: 0,
					wantFeedEntities: []*gtfsrt.FeedEntity{
						wantAlertFeedEntity(
							ptr("Alert header"),
							ptr("Alert description"),
							nil,
							ptr(stopID14St),
							nil,
							nil),
					},
				},
			},
		},
		{
			name: "alert with single line",
			updates: []alertUpdate{
				{
					incidents: []Incident{
						{
							IncidentMessage: &portauthorityv1.GetIncidentsResponse_Incidentmessage{
								Subject:    "Alert header",
								PreMessage: "Alert description",
								FormVariableItems: []*portauthorityv1.GetIncidentsResponse_Formvariableitems{
									{
										VariableName: "Lines",
										Val:          []string{"HOB-33S"},
									},
								},
							},
						},
					},
					wantErrs: 0,
					wantFeedEntities: []*gtfsrt.FeedEntity{
						wantAlertFeedEntity(
							ptr("Alert header"),
							ptr("Alert description"),
							nil,
							nil,
							&[]string{routeID1},
							nil),
					},
				},
			},
		},
		{
			name: "alert with multiple lines",
			updates: []alertUpdate{
				{
					incidents: []Incident{
						{
							IncidentMessage: &portauthorityv1.GetIncidentsResponse_Incidentmessage{
								Subject:    "Alert header",
								PreMessage: "Alert description",
								FormVariableItems: []*portauthorityv1.GetIncidentsResponse_Formvariableitems{
									{
										VariableName: "Lines",
										Val:          []string{"HOB-33S", "HOB-WTC"},
									},
								},
							},
						},
					},
					wantErrs: 0,
					wantFeedEntities: []*gtfsrt.FeedEntity{
						wantAlertFeedEntity(
							ptr("Alert header"),
							ptr("Alert description"),
							nil,
							nil,
							&[]string{routeID1, routeID2},
							nil),
					},
				},
			},
		},
		{
			name: "alert with station and lines",
			updates: []alertUpdate{
				{
					incidents: []Incident{
						{
							IncidentMessage: &portauthorityv1.GetIncidentsResponse_Incidentmessage{
								Subject:    "Alert header",
								PreMessage: "Alert description",
								FormVariableItems: []*portauthorityv1.GetIncidentsResponse_Formvariableitems{
									{
										VariableName: "Station",
										Val:          []string{"14S"},
									},
									{
										VariableName: "Lines",
										Val:          []string{"HOB-33S", "HOB-WTC"},
									},
								},
							},
						},
					},
					wantErrs: 0,
					wantFeedEntities: []*gtfsrt.FeedEntity{
						wantAlertFeedEntity(
							ptr("Alert header"),
							ptr("Alert description"),
							nil,
							ptr(stopID14St),
							&[]string{routeID1, routeID2},
							nil),
					},
				},
			},
		},
		{
			name: "error in client",
			updates: []alertUpdate{
				{
					clientError: fmt.Errorf("client error"),
					wantErrs:    1,
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := mockSourceClient{
				stationToStopID: map[sourceapi.Station]string{
					sourceapi.Station_FOURTEENTH_STREET: stopID14St,
					sourceapi.Station_HOBOKEN:           stopIDHoboken,
				},
				routeToRouteID: map[sourceapi.Route]string{
					sourceapi.Route_HOB_33:  routeID1,
					sourceapi.Route_HOB_WTC: routeID2,
				},
				stationToTrains: map[sourceapi.Station][]Train{
					sourceapi.Station_FOURTEENTH_STREET: nil,
					sourceapi.Station_HOBOKEN:           nil,
				},
			}
			portAuthorityClient := mockPortAuthorityClient{
				incidents: []Incident{},
			}
			ctx := context.Background()
			updateSignal := make(chan []error, 1)

			c := clock.NewMock()
			staticData, err := GetStaticData(ctx, &client)
			feed, err := NewPortAuthorityAlertFeed(ctx, c, 5*time.Second, &portAuthorityClient, staticData, func(msg *gtfsrt.FeedMessage, requestErrs []error) {
				updateSignal <- requestErrs
			})
			if err != nil {
				t.Fatalf("NewAlertFeed() err got=%v, want=<nil>", err)
			}
			requestErrs := <-updateSignal
			if numErrs := len(requestErrs); numErrs != 0 {
				t.Errorf("callback errs got=%d, want=0", numErrs)
			}
			for _, update := range tc.updates {
				portAuthorityClient.incidents = update.incidents
				portAuthorityClient.error = update.clientError
				c.Add(5 * time.Second)
				requestErrs = <-updateSignal
				if numErrs := len(requestErrs); numErrs != update.wantErrs {
					t.Errorf("callback errs got=%d, want=%d", numErrs, update.wantErrs)
				}
				b := feed.Get()
				var gotMsg gtfsrt.FeedMessage
				if err := proto.Unmarshal(b, &gotMsg); err != nil {
					t.Errorf("proto.Unmarshal() errs got=%v, want=<nil>", err)
				}
				now := uint64(c.Now().Unix())
				wantMsg := gtfsrt.FeedMessage{
					Header: &gtfsrt.FeedHeader{
						GtfsRealtimeVersion: ptr("0.2"),
						Incrementality:      gtfsrt.FeedHeader_FULL_DATASET.Enum(),
						Timestamp:           &now,
					},
					Entity: update.wantFeedEntities,
				}
				if diff := cmp.Diff(&gotMsg, &wantMsg,
					protocmp.Transform(),
					protocmp.IgnoreFields(&gtfsrt.FeedEntity{}, "id"),
				); diff != "" {
					t.Errorf("GTFS realtime feed got != want, diff=%s", diff)
				}
			}

		})
	}
}

func sourceTrain(route sourceapi.Route, direction sourceapi.Direction, projectedArrival int, lastUpdated int) Train {
	return Train(&sourceapi.GetUpcomingTrainsResponse_UpcomingTrain{
		Route:            route,
		Direction:        direction,
		ProjectedArrival: makeTimestamppb(projectedArrival),
		LastUpdated:      makeTimestamppb(lastUpdated),
	})
}

func wantFeedEntity(routeID string, directionID uint32, stopID string, arrival int, lastUpdated int) *gtfsrt.FeedEntity {
	u := uint64(*makeUnix(lastUpdated))
	return &gtfsrt.FeedEntity{
		TripUpdate: &gtfsrt.TripUpdate{
			Trip: &gtfsrt.TripDescriptor{
				RouteId:     &routeID,
				DirectionId: &directionID,
			},
			Timestamp: &u,
			StopTimeUpdate: []*gtfsrt.TripUpdate_StopTimeUpdate{
				{
					StopId: &stopID,
					Arrival: &gtfsrt.TripUpdate_StopTimeEvent{
						Time: makeUnix(arrival),
					},
				},
			},
		},
	}
}

func wantAlertFeedEntity(
	headerText *string,
	descriptionText *string,
	agencyID *string,
	stopID *string,
	routeIDs *[]string,
	effect *gtfs.Alert_Effect) *gtfsrt.FeedEntity {

	entity := &gtfsrt.FeedEntity{
		Alert: &gtfsrt.Alert{},
	}

	if headerText != nil {
		entity.Alert.HeaderText = &gtfs.TranslatedString{
			Translation: []*gtfs.TranslatedString_Translation{
				{
					Text:     headerText,
					Language: ptr("en"),
				},
			},
		}
	}

	if descriptionText != nil {
		entity.Alert.DescriptionText = &gtfs.TranslatedString{
			Translation: []*gtfs.TranslatedString_Translation{
				{
					Text:     descriptionText,
					Language: ptr("en"),
				},
			},
		}
	}

	var informedEntities []*gtfsrt.EntitySelector
	if stopID != nil {
		informedEntities = append(informedEntities, &gtfsrt.EntitySelector{
			StopId: stopID,
		})
	}

	if routeIDs != nil {
		for routeIDIdx := range *routeIDs {
			informedEntities = append(informedEntities, &gtfsrt.EntitySelector{
				RouteId: &(*routeIDs)[routeIDIdx],
			})
		}
	}

	if agencyID != nil {
		informedEntities = append(informedEntities, &gtfsrt.EntitySelector{
			AgencyId: agencyID,
		})
	}

	entity.Alert.InformedEntity = informedEntities
	entity.Alert.Effect = effect

	return entity
}

func makeTime(t int) time.Time {
	return time.Date(2023, time.February, 26, 10, t, 0, 0, time.UTC)
}

func makeUnix(t int) *int64 {
	a := makeTime(t).Unix()
	return &a
}

func makeTimestamppb(t int) *timestamppb.Timestamp {
	return timestamppb.New(makeTime(t))
}

type update struct {
	data             map[sourceapi.Station][]Train
	wantErrs         int
	wantFeedEntities []*gtfsrt.FeedEntity
}

type mockSourceClient struct {
	stationToStopID map[sourceapi.Station]string
	routeToRouteID  map[sourceapi.Route]string
	stationToTrains map[sourceapi.Station][]Train
}

func (m *mockSourceClient) GetStationToStopId(context.Context) (map[sourceapi.Station]string, error) {
	return m.stationToStopID, nil
}

func (m *mockSourceClient) GetRouteToRouteId(context.Context) (map[sourceapi.Route]string, error) {
	return m.routeToRouteID, nil
}

func (m *mockSourceClient) GetTrainsAtStation(_ context.Context, s sourceapi.Station) ([]Train, error) {
	trains, ok := m.stationToTrains[s]
	if !ok {
		return nil, fmt.Errorf("error getting trains at station %s", s)
	}
	return trains, nil
}

type alertUpdate struct {
	incidents        []Incident
	clientError      error
	wantErrs         int
	wantFeedEntities []*gtfsrt.FeedEntity
}

type mockPortAuthorityClient struct {
	incidents []Incident
	error     error
}

func (m *mockPortAuthorityClient) GetIncidents(context.Context) ([]Incident, error) {
	if m.error != nil {
		return nil, m.error
	}

	return m.incidents, nil
}

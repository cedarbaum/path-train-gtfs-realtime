package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"time"

	"github.com/benbjohnson/clock"
	pathgtfsrt "github.com/jamespfennell/path-train-gtfs-realtime"
	gtfs "github.com/jamespfennell/path-train-gtfs-realtime/proto/gtfsrt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

//go:embed index.html
var indexHTMLPage string

var port = flag.Int("port", 8080, "the port to bind the HTTP server to")
var tripUpdatePeriod = flag.Duration("trip_update_period", 5*time.Second, "how often to update the feed")
var alertUpdatePeriod = flag.Duration("alert_update_period", 30*time.Second, "how often to update the feed")
var timeoutPeriod = flag.Duration("timeout_period", 5*time.Second, "maximum duration to wait for a response from the source API")
var alertTimeoutPeriod = flag.Duration("alert_timeout_period", 30*time.Second, "maximum duration to wait for a response from the source API")
var useHTTPSourceAPI = flag.Bool("use_http_source_api", false, "use the HTTP source API instead of the default gRPC API")
var publishPortAuthorityAlerts = flag.Bool("publish_port_authority_alerts", false, "publish alerts from the Port Authorities Everbridge feed")

var numUpdatesCounter = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "path_train_gtfsrt_num_updates",
		Help: "Number of completed updates",
	},
)
var numRequestErrs = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "path_train_gtfsrt_num_source_api_errors",
		Help: "Number of errors when retrieving realtime data from the source API",
	},
)
var lastUpdateGauge = promauto.NewGauge(
	prometheus.GaugeOpts{
		Name: "path_train_gtfsrt_last_update",
		Help: "Time of the last completed update",
	},
)
var numTripStopTimesGauge = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "path_train_gtfsrt_num_trip_stop_times",
		Help: "Number of trip stop times per station and direction",
	},
	[]string{"stop_id", "direction"},
)
var tripUpdateFeedRequestCounter = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "path_train_gtfsrt_trip_feed_num_requests",
		Help: "Number of times the GTFS-RT trip update feed has been requested",
	},
	[]string{"code"},
)
var portAuthorityAlertFeedRequestCounter = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "path_train_gtfsrt_port_authority_alert_feed_num_requests",
		Help: "Number of times the GTFS-RT Port Authority alert feed has been requested",
	},
	[]string{"code"},
)

func main() {
	flag.Parse()
	if err := run(context.Background()); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	var sourceClient pathgtfsrt.SourceClient
	if *useHTTPSourceAPI {
		fmt.Println("Source API: HTTP")
		sourceClient = pathgtfsrt.NewHttpSourceClient(*timeoutPeriod)
	} else {
		fmt.Println("Source API: gRPC")
		grpcClient, err := pathgtfsrt.NewGrpcSourceClient(*timeoutPeriod)
		if err != nil {
			return err
		}
		defer grpcClient.Close()
		sourceClient = grpcClient
	}

	staticData, err := pathgtfsrt.GetStaticData(ctx, sourceClient)
	if err != nil {
		return err
	}

	tripUpdateFeed, err := pathgtfsrt.NewTripUpdateFeed(ctx, clock.New(), *tripUpdatePeriod, sourceClient, staticData, recordTripUpdate)
	if err != nil {
		return fmt.Errorf("failed to initialize feed: %s", err)
	}

	http.HandleFunc("/", rootHandler)
	http.Handle("/gtfsrt", promhttp.InstrumentHandlerCounter(tripUpdateFeedRequestCounter, tripUpdateFeed))
	http.Handle("/metrics", promhttp.Handler())

	if *publishPortAuthorityAlerts {
		fmt.Println("Publishing Port Authority alerts")
		portAuthorityClient := pathgtfsrt.NewPortAuthorityClient(*alertTimeoutPeriod)
		portAuthorityAlertFeed, err := pathgtfsrt.NewPortAuthorityAlertFeed(ctx, clock.New(), *alertUpdatePeriod, portAuthorityClient, staticData, recordAlertUpdate)
		if err != nil {
			return err
		}
		http.Handle("/port_authority_alerts", promhttp.InstrumentHandlerCounter(tripUpdateFeedRequestCounter, portAuthorityAlertFeed))
	}

	return http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.New("index.html").Parse(indexHTMLPage)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	err = tmpl.Execute(w, "data goes here")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func recordTripUpdate(msg *gtfs.FeedMessage, errs []error) {
	numTripStopTimesGauge.Reset()
	for _, entity := range msg.GetEntity() {
		directionID := "NY"
		if entity.GetTripUpdate().GetTrip().GetDirectionId() == 0 {
			directionID = "NJ"
		}
		for _, stopTimeUpdate := range entity.GetTripUpdate().GetStopTimeUpdate() {
			stopID := stopTimeUpdate.GetStopId()
			numTripStopTimesGauge.WithLabelValues(stopID, directionID).Inc()
		}
	}
	numUpdatesCounter.Inc()
	numRequestErrs.Add(float64(len(errs)))
	lastUpdateGauge.SetToCurrentTime()
}

func recordAlertUpdate(msg *gtfs.FeedMessage, errs []error) {
}

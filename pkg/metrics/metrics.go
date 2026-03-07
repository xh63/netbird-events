package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	// custom registry to avoid exposing all golang metrics
	MyRegistry = prometheus.NewRegistry()

	// EventsProcessed counts events processed per poll cycle, labeled by error status
	EventsProcessed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "eventsproc_events_processed_total",
			Help: "Total number of events processed",
		},
		[]string{"error"},
	)

	// ProcessingDuration records how long each poll cycle takes end-to-end
	ProcessingDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "eventsproc_processing_duration_seconds",
			Help:    "Duration of each processing cycle in seconds",
			Buckets: prometheus.DefBuckets,
		},
	)

	// BatchSize records how many events were fetched per batch
	BatchSize = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "eventsproc_batch_size_events",
			Help:    "Number of events fetched per batch",
			Buckets: []float64{10, 50, 100, 250, 500, 1000},
		},
	)

	// LastEventID tracks the ID of the last successfully processed event
	LastEventID = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "eventsproc_last_event_id",
			Help: "ID of the last successfully processed event",
		},
	)

	// CheckpointLagSeconds is the most important metric: seconds between the
	// last processed event timestamp and now. A growing value means processing
	// is falling behind or has stopped entirely.
	CheckpointLagSeconds = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "eventsproc_checkpoint_lag_seconds",
			Help: "Seconds between last processed event timestamp and now",
		},
	)

	// IsLeader is 1 when this instance holds the Redis leader lock, 0 otherwise.
	// Only meaningful when cluster mode is enabled.
	IsLeader = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "eventsproc_leader",
			Help: "1 if this instance currently holds the leader lock, 0 otherwise",
		},
	)

	// LastPollTime is the Unix timestamp of the last completed poll cycle.
	// Updated every cycle regardless of whether events were found.
	// Use this to detect if the service has stopped running:
	//   time() - eventsproc_last_poll_timestamp_seconds > polling_interval + buffer
	LastPollTime = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "eventsproc_last_poll_timestamp_seconds",
			Help: "Unix timestamp of the last completed poll cycle",
		},
	)

	// DBQueryDuration records how long each database operation takes.
	// Labeled by operation: "get_events", "save_checkpoint".
	DBQueryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "eventsproc_db_query_duration_seconds",
			Help:    "Duration of database queries in seconds",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
		},
		[]string{"operation"},
	)
)

func init() {
	MyRegistry.MustRegister(EventsProcessed)
	MyRegistry.MustRegister(ProcessingDuration)
	MyRegistry.MustRegister(BatchSize)
	MyRegistry.MustRegister(LastEventID)
	MyRegistry.MustRegister(CheckpointLagSeconds)
	MyRegistry.MustRegister(IsLeader)
	MyRegistry.MustRegister(LastPollTime)
	MyRegistry.MustRegister(DBQueryDuration)
}

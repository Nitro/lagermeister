package event

// These are the metrics we receive on the wire.
//
// Valid values for Aggregate:
//  * Average
//  * Total
type MetricEvent struct {
	Timestamp  int64
	Value      float64
	Sender     string
	SourceIP   string `omitempty:"true"`
	MetricType string
	Threshold  map[string]float64 `omitempty:"true"`
	Aggregate  string
}

package event

// These are the metrics we receive on the wire
type MetricEvent struct {
	Timestamp  int64
	Value      float64
	Sender     string
	SourceIP   string `omitempty:"true"`
	MetricType string
	Threshold  map[string]float64 `omitempty:"true"`
}

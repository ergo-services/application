package radar

import "ergo.services/ergo/gen"

const (
	DefaultPort        uint16 = 9090
	DefaultHost        string = "localhost"
	DefaultHealthPath  string = "/health"
	DefaultMetricsPath string = "/metrics"
	DefaultPoolSize    int64  = 3

	Name gen.Atom = "radar"

	nameSup         gen.Atom = "radar_sup"
	nameHealth      gen.Atom = "radar_health"
	nameMetricsErgo gen.Atom = "radar_metrics_ergo"
	nameMetrics     gen.Atom = "radar_metrics"
	nameWeb         gen.Atom = "radar_web"
)

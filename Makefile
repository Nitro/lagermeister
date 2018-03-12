ALL_BINS=http_receiver/http_receiver http_subscriber/http_subscriber log_generator/log_generator stats_proxy/stats_proxy tcp_receiver/tcp_receiver

all: $(ALL_BINS)

http_receiver/http_receiver:
	cd http_receiver && go build

http_subscriber/http_subscriber:
	cd http_subscriber && go build

log_generator/log_generator:
	cd log_generator && go build

stats_proxy/stats_proxy:
	cd stats_proxy && go build

tcp_receiver/tcp_receiver:
	cd tcp_receiver && go build

clean:
	rm http_receiver/http_receiver
	rm http_subscriber/http_subscriber
	rm log_generator/log_generator
	rm stats_proxy/stats_proxy
	rm tcp_receiver/tcp_receiver

ALL_BINS=http_receiver/http_receiver http_subscriber/http_subscriber log_generator/log_generator stats_proxy/stats_proxy tcp_receiver/tcp_receiver

all: $(ALL_BINS)
	mkdir -p bin
	cp $(ALL_BINS) bin/

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
	rm -f http_receiver/http_receiver
	rm -f http_subscriber/http_subscriber
	rm -f log_generator/log_generator
	rm -f stats_proxy/stats_proxy
	rm -f tcp_receiver/tcp_receiver

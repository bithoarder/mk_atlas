export GOPATH=$(shell echo $$PWD)

GOBINS	= mk_atlas

all:: gobuild

.PHONY: gobuild
gobuild:
	@go install $(GOBINS)

.PHONY: profile
profile: /tmp/mk_atlas.prof
	@go tool pprof ./bin/mk_atlas /tmp/mk_atlas.prof --web

/tmp/mk_atlas.prof: gobuild
	@sudo bash -c 'for i in /sys/devices/system/cpu/cpu[0-7] ; do echo performance > $$i/cpufreq/scaling_governor ; done'
	@./bin/mk_atlas -cpuprofile=$@ test_images/data2/penguin/*/*.png
	@sudo bash -c 'for i in /sys/devices/system/cpu/cpu[0-7] ; do echo ondemand > $$i/cpufreq/scaling_governor ; done'

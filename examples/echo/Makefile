test-client:
	mkdir -p reports
	mkdir -p reports/clients
	docker run -it --rm \
  	-v $(PWD)/config:/config \
    -v $(PWD)/reports:/reports \
    --add-host host.docker.internal:host-gateway \
		-p 9001:9001 \
    crossbario/autobahn-testsuite \
    wstest -m fuzzingserver -s /config/fuzzingserver.json --debug

test-server:
	mkdir -p reports
	docker run -it --rm \
  	-v $(PWD)/config:/config \
    -v $(PWD)/reports:/reports \
    --add-host host.docker.internal:host-gateway \
    crossbario/autobahn-testsuite \
    wstest -m fuzzingclient -s /config/fuzzingclient.json


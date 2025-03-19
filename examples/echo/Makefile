test:
	mkdir -p reports
	docker run -it --rm \
  	-v $(PWD)/config:/config \
    -v $(PWD)/reports:/reports \
    --add-host host.docker.internal:host-gateway \
    crossbario/autobahn-testsuite \
    wstest -m fuzzingclient -s /config/fuzzingclient.json


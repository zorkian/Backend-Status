backend-status-server:
	6g backend-status-server.go
	6l -o backend-status-server backend-status-server.6

clean:
	rm -f *.6 backend-status-server

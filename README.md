# bluelink_proxy_server
Proxy api server to call blue link backend services to start/stop climate control, etc. A user can then create SIRI shortcuts or IFTTT webhooks to directly send web calls to this proxy server to control blue link functions. 

This web proxy service uses the [GO bluelink wrapper](https://github.com/TaiPhamD/bluelink_go)

# APIs
- /api/start_climate
- /api/stop_climate
- /api/get_odometer
# Usage
## pre-requisites
- go lang compiler 1.16+
- (optional) an SSL cert crt and private key if you want to serve this proxy server as HTTPS
## Compile
```
git clone https://github.com/TaiPhamD/bluelink_proxy_server.git
cd bluelink_proxy_server
go mod download
go build
```
## setup config.json
- add your info in config_example.json
```
{
    "tls": true,
    "port": "8090",
    "rate_limit": 1,
    "rate_burst": 1,
    "fullchain": "/ssd1/apps/.lego/certificates/my_domain.duckdns.org.crt",
    "priv_key": "/ssd1/apps/.lego/certificates/my_domain.duckdns.org.key",
    "api_key": "my_secret_key",
    "username": "my_email@gmail.com",
    "pin": "1234",
    "password": "my_password",
    "vin": "KMXXXXXXXX"
}
```
## install
- run install_linux.sh:
  - it will the compiled binary and config.json to /opt/bluelink/
  - It will start a systemd service (if your linux doesn't have systemd then you need to adapt the install_linux.sh to make it work)


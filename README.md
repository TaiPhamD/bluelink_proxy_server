# bluelink_proxy_server
Proxy api server to call blue link backend services to start/stop climate control, etc. A user can then create SIRI shortcuts or IFTTT webhooks to directly send web calls to this proxy server to control blue link functions. 

This web proxy service uses the [GO bluelink wrapper](https://github.com/TaiPhamD/bluelink_go)

# APIs
- /api/start_climate
- /api/stop_climate
- /api/get_odometer

# setup config.json

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

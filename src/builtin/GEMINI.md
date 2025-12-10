This library should include only Components that do not depend on external libraries to function (only standard Go).
It should support:
- Uptime checks -- tracking how many seconds have passed since the component started;
- Healthcheck of an URL that issues a plain HTTP GET request on that URL, and checks if the HTTP status code is >= 200 and <300 (OK status). All non-OK status codes should be treated as errors.
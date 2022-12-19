package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"golang.org/x/time/rate"
)

// The data struct to decode config.json
type Config struct {
	TLS        bool   `json:"tls"`
	Port       string `json:"port"`
	RateLimit  int    `json:"rate_limit"`
	RateBurst  int    `json:"rate_burst"`
	Fullchain  string `json:"fullchain"`
	PrivKey    string `json:"priv_key"`
	APIKey     string `json:"api_key"`
	APIKeyHash [32]byte
	Username   string `json:"username"`
	Pin        int    `json:"pin"`
	Password   string `json:"password"`
	VIN        string `json:"vin"`
}

type Params struct {
	APIKey string `json:"api_key"`
}

type ctxBlueLinkParam struct{}

var MyConfig Config
var limiter *rate.Limiter

func ParseConfig() (Config, error) {

	var result Config
	var content []byte

	// get path of running executable
	filename, err := os.Executable()
	if err != nil {
		panic(err)
	}
	exPath := filepath.Dir(filename)
	// build file path as wd + '/config.json'
	filePath := exPath + "/config.json"
	content, err = ioutil.ReadFile(filePath)
	if err != nil {
		return Config{}, err
	}
	err = json.Unmarshal(content, &result)
	result.APIKeyHash = sha256.Sum256([]byte(result.APIKey))
	if err != nil {
		return Config{}, err
	}

	return result, nil
}

// define limit middleware that checks limiter
func Limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// check if request is allowed by limiter
		if !limiter.Allow() {
			// log too many requests and api path
			log.Println(r.URL.Path, "Too many requests")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte("429 - Too Many Requests"))
			return
		}

		next.ServeHTTP(w, r)
	})
}

// auth middleware to check api key
func Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		//check password
		var params Params
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&params)
		if err != nil {
			log.Println(r.URL.Path, "Error decoding JSON: ", err)
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("400 - Bad Request"))
			return
		}
		paramApiHash := sha256.Sum256([]byte(params.APIKey))
		if !bytes.Equal(paramApiHash[:], MyConfig.APIKeyHash[:]) {
			log.Println(r.URL.Path, "API Key doesn't match")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("401 - Unauthorized"))
			return
		}
		// add param to r context
		ctx := r.Context()
		ctx = context.WithValue(ctx, ctxBlueLinkParam{}, params)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

var base_url = "https://owners.hyundaiusa.com"

var jwt_token string
var jwt_id string
var reg_id string
var expires_at int64

func StartClimateHandler(w http.ResponseWriter, r *http.Request) {
	// check if expires_at is 30 min in the future otherwise call login again
	if time.Now().Unix() > (expires_at + 1800) {
		login()
	}
	// start climate request
	req, err := http.NewRequest("POST", base_url+"/bin/common/remoteAction", nil)
	if err != nil {
		log.Println("Error starting climate req: ", err)
		return
	}
	// add headers
	req.Header.Add("CSRF-Token", "undefined")
	req.Header.Add("accept-language", "en-US,en;q=0.9")
	req.Header.Add("X-Requested-With", "XMLHttpRequest")
	req.Header.Add("Referer", "https://owners.hyundaiusa.com/us/en/page/blue-link.html")
	req.Header.Add("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.2 Safari/605.1.15")
	req.Header.Add("Host", "owners.hyundaiusa.com")
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Add("Origin", "https://owners.hyundaiusa.com")
	req.Header.Add("Cookie", "jwt_token="+jwt_id+"; s_name="+MyConfig.Username)
	// add form data
	q := req.URL.Query()
	q.Add("vin", MyConfig.VIN)
	q.Add("username", MyConfig.Username)
	q.Add("token", jwt_id)
	// convert MyConfig.Pin to string
	pin_string := strconv.Itoa(MyConfig.Pin)
	q.Add("pin", pin_string)
	q.Add("service", "postRemoteFatcStart")
	q.Add("url", "https://owners.hyundaiusa.com/us/en/page/blue-link.html")
	q.Add("regId", reg_id)
	q.Add("gen", "2")
	q.Add("airCtrl", "true")
	q.Add("igniOnDuration", "NaN")
	q.Add("airTempvalue", "72")
	q.Add("defrost", "false")
	q.Add("heating1", "0")
	q.Add("seatHeaterVentInfo", `{"drvSeatHeatState":"2"}`)
	req.URL.RawQuery = q.Encode()
	// send request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Println("Error starting climate: ", err)
		return
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("Error reading start climate: ", err)
		return
	}
	log.Println("Start climate response: ", string(body))
	// decode body as json
	var start_climate_response map[string]interface{}
	err = json.Unmarshal(body, &start_climate_response)
	if err != nil {
		log.Println("Error decoding start climate response: ", err)
		return
	}
	// read "E_IFRESULT" from start_climate_response
	if start_climate_response["E_IFRESULT"] == "Z:Success" {
		w.Write([]byte("Climate started"))
		// write status 200
		w.WriteHeader(http.StatusOK)
	} else {
		w.Write([]byte("Error starting climate"))
		// write status 500
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func StopClimateHandler(w http.ResponseWriter, r *http.Request) {
	if time.Now().Unix() > (expires_at + 1800) {
		login()
	}

	// stop climate request
	req, err := http.NewRequest("POST", base_url+"/bin/common/remoteAction", nil)
	if err != nil {
		log.Println("Error stopping climate req: ", err)
		return
	}
	// add headers
	req.Header.Add("CSRF-Token", "undefined")
	req.Header.Add("accept-language", "en-US,en;q=0.9")
	req.Header.Add("X-Requested-With", "XMLHttpRequest")
	req.Header.Add("Referer", "https://owners.hyundaiusa.com/us/en/page/blue-link.html")
	req.Header.Add("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.2 Safari/605.1.15")
	req.Header.Add("Host", "owners.hyundaiusa.com")
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Add("Origin", "https://owners.hyundaiusa.com")
	req.Header.Add("Cookie", "jwt_token="+jwt_id+"; s_name="+MyConfig.Username)
	// add form data
	q := req.URL.Query()
	q.Add("vin", MyConfig.VIN)
	q.Add("username", MyConfig.Username)
	q.Add("token", jwt_id)
	// convert MyConfig.Pin to string
	pin_string := strconv.Itoa(MyConfig.Pin)
	q.Add("pin", pin_string)
	q.Add("service", "postRemoteFatcStop")
	q.Add("url", "https://owners.hyundaiusa.com/us/en/page/blue-link.html")
	q.Add("regId", reg_id)
	q.Add("gen", "2")
	req.URL.RawQuery = q.Encode()
	// send request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Println("Error stopping climate: ", err)
		return
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("Error reading stop climate: ", err)
		return
	}
	log.Println("stop climate response: ", string(body))
	// decode body as json
	var start_climate_response map[string]interface{}
	err = json.Unmarshal(body, &start_climate_response)
	if err != nil {
		log.Println("Error decoding stop climate response: ", err)
		return
	}
	// read "E_IFRESULT" from start_climate_response
	if start_climate_response["E_IFRESULT"] == "Z:Success" {
		w.Write([]byte("Climate stopped"))
		// write status 200
		w.WriteHeader(http.StatusOK)
	} else {
		w.Write([]byte("Error stopping climate"))
		// write status 500
		w.WriteHeader(http.StatusInternalServerError)
	}

}

func GetOdometerHandler(w http.ResponseWriter, r *http.Request) {
	if time.Now().Unix() > (expires_at + 1800) {
		login()
	}
	_, odometer, err := get_reg_id()
	if err != nil {
		w.Write([]byte("Error getting odometer reading"))
		// write status 500
		w.WriteHeader(http.StatusInternalServerError)		
	} else {
		w.Write([]byte("Odometer: " + odometer))
		// write status 200
		w.WriteHeader(http.StatusOK)		
	}



}

func get_jwt_token() (string, error) {
	/*
		curl --location --request GET 'https://owners.hyundaiusa.com/etc/designs/ownercommon/us/token.json'
	*/
	// create request based on curl command above
	req, err := http.NewRequest("GET", base_url+"/etc/designs/ownercommon/us/token.json", nil)
	if err != nil {
		log.Println("Error getting jwt_token req: ", err)
		return "", err
	}
	// get jwt_token from json response
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Println("Error getting jwt_token: ", err)
		return "", err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("Error reading jwt_token: ", err)
		return "", err
	}
	var result map[string]interface{}
	json.Unmarshal([]byte(body), &result)
	jwt_token = result["jwt_token"].(string)
	log.Println("jwt_token: ", jwt_token)
	// create request based on curl command above and pass in jwt_token as the csrf_token header
	req, err = http.NewRequest("GET", base_url+"/libs/granite/csrf/token.json", nil)
	if err != nil {
		log.Println("Error getting csrf_token req: ", err)
		return "", err
	}
	req.Header.Add("csrf_token", jwt_token)
	// check response status
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		log.Println("Error getting csrf_token: ", err)
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Println("Error getting csrf_token: ", resp.Status)
		return "", err
	}
	return jwt_token, nil

}

func get_jwt_id() (string, error) {
	// create new request based on this curl code

	req, err := http.NewRequest("POST", base_url+"/bin/common/connectCar", nil)
	if err != nil {
		log.Println("Error getting csrf_token req: ", err)
		return "", err
	}
	// Add url query params to request
	q := req.URL.Query()
	q.Add(":cq_csrf_token", jwt_token)
	q.Add("url", base_url+"/us/en/index.html")
	q.Add("username", MyConfig.Username)
	q.Add("password", MyConfig.Password)
	req.URL.RawQuery = q.Encode()
	// check response status
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Println("Error getting csrf_token: ", err)
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Println("Error logging in ", resp.Status)
		return "", err
	}
	// print response body as json
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("Error reading login response: ", err)
	}
	var login_result map[string]interface{}
	json.Unmarshal([]byte(body), &login_result)
	// print login_result["RESPONSE_STRING"]["jwt_id"]
	// print jwt_id from JSON
	jwt_id = login_result["RESPONSE_STRING"].(map[string]interface{})["jwt_id"].(string)
	log.Println("jwt_id: ", jwt_id)
	// remove first 4 characters from jwt_id if it contains "JWT-" at the beginning
	var jwt_id_truncated string
	jwt_id_truncated = jwt_id
	if strings.HasPrefix(jwt_id, "JWT-") {
		jwt_id_truncated = jwt_id[4:]
	}
	// decode JWT token and get expiration date from "exp" field
	token, err := jwt.Parse(jwt_id_truncated, func(token *jwt.Token) (interface{}, error) {
		hmacSampleSecret := []byte("secret")
		return hmacSampleSecret, nil
	})
	if err != nil {
		log.Println("Error parsing jwt_id: ", err)
	}
	// print expiration date
	expires_at = int64(token.Claims.(jwt.MapClaims)["exp"].(float64) / 1000)
	log.Println("Raw expiration date: ", expires_at)
	log.Println("Expiration date: ", time.Unix(expires_at, 0))

	return jwt_id, nil
}

func get_reg_id() (string, string, error) {
	// create new request
	req, err := http.NewRequest("POST", "https://owners.hyundaiusa.com/bin/common/MyAccountServlet", nil)
	if err != nil {
		log.Println("Error creating new request: ", err)
		return "","", err
	}
	// set headers
	req.Header.Set("CSRF-Token", "undefined")
	req.Header.Set("accept-language", "en-US,en;q=0.9")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Referer", "https://owners.hyundaiusa.com/content/myhyundai/us/en/page/dashboard.html")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.2 Safari/605.1.15")
	req.Header.Set("Host", "owners.hyundaiusa.com")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("Origin", "https://owners.hyundaiusa.com")
	req.Header.Set("Cookie", "jwt_token="+jwt_id+"; s_name="+MyConfig.Username)
	// set form data
	q := req.URL.Query()
	q.Add("username", MyConfig.Username)
	q.Add("token", jwt_id)
	q.Add("service", "getOwnerInfoService")
	q.Add("url", "https://owners.hyundaiusa.com/us/en/page/dashboard.html")
	req.URL.RawQuery = q.Encode()
	// send request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Println("Error sending request: ", err)
		return "","", err
	}
	defer resp.Body.Close()
	// check response status
	if resp.StatusCode != 200 {
		log.Println("Error response status: ", resp.StatusCode)
		return "","", errors.New("Error response status: " + strconv.Itoa(resp.StatusCode))
	}

	log.Println("Response Status: ", resp.Status)

	// read response
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("Error reading response: ", err)
		return "", "",err
	}
	// parse response
	var result map[string]interface{}
	err = json.Unmarshal(body, &result)
	if err != nil {
		log.Println("Error parsing response: ", err)
		return "","", err
	}

	// get response info
	response_info := result["RESPONSE_STRING"].(map[string]interface{})
	log.Println("Response Info: ", response_info)
	// get owners info and find vehicle matching VIN
	owner_info := response_info["OwnersVehiclesInfo"]
	log.Println("Owner Info: ", owner_info)
	// loop through vehicles
	for _, vehicle := range owner_info.([]interface{}) {
		vehicle_info := vehicle.(map[string]interface{})
		if vehicle_info["VinNumber"].(string) == MyConfig.VIN {
			// get vehicle id
			_reg_id := vehicle_info["RegistrationID"].(string)
			log.Println("Reg ID: ", _reg_id)
			_odometer := vehicle_info["Mileage"].(string)
			return _reg_id,_odometer, nil
		}
	}
	return "", "",errors.New("reg id not found")
}

func login() {
	var err error
	jwt_token, err = get_jwt_token()
	if err != nil {
		log.Println("Error getting jwt_token: ", err)
	}
	jwt_id, err = get_jwt_id()
	if err != nil {
		log.Println("Error getting jwt_id: ", err)
	}
	reg_id, _, err = get_reg_id()
	if err != nil {
		log.Println("Error getting reg_id: ", err)
	}

}

func Setup() (Config, http.Handler, error) {
	var err error
	// parse config
	MyConfig, err = ParseConfig()
	if err != nil {
		fmt.Println("Error parsing config.json: ", err)
		log.Fatal("Error parsing config.json: ", err)
	}
	log.Println("Config parsed successfully")
	log.Println("Rate limit: ", MyConfig.RateLimit)
	log.Println("Rate burst: ", MyConfig.RateBurst)
	limiter = rate.NewLimiter(rate.Limit(MyConfig.RateLimit), MyConfig.RateBurst)
	mux := http.NewServeMux()

	// handle Restart start_climate
	mux.HandleFunc("/api/start_climate", StartClimateHandler)
	// handle Suspend stop_climate
	mux.HandleFunc("/api/stop_climate", StopClimateHandler)
	// get odometer
	mux.HandleFunc("/api/get_odometer", GetOdometerHandler)
	handler := Limit(Auth(mux))

	login()
	return MyConfig, handler, err
}

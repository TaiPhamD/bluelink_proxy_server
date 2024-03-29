package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/TaiPhamD/bluelink_go"
	"github.com/TaiPhamD/bluelink_go/api"

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
	Pin        string `json:"pin"`
	Password   string `json:"password"`
	VIN        string `json:"vin"`
}

type Params struct {
	APIKey         string  `json:"api_key"`
	AirTemperature *string `json:"air_temperature,omitempty"`
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

// middleware to refresh bluelink token if it expires
func RefreshBlueLink(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// check if current time is within 25 minutes of token expiration
		// then refresh the token
		expirationTime := time.Unix(bluelink_auth.ExpiresAt, 0)
		var err error
		if time.Now().After(expirationTime.Add(-1 * time.Minute)) {
			//  relogin if token is within 1 minute of expiration date
			bluelink_auth, err = bluelink_go.Login(MyConfig.Username, MyConfig.Password, MyConfig.Pin, MyConfig.VIN)
			if err != nil {
				log.Println(r.URL.Path, "could not login: ", err)
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("500 - StatusInternalServerError"))
				return
			}

		} else {
			// token hasn't expired yet so just refresh
			bluelink_auth, err = bluelink_go.RefreshToken(bluelink_auth)
			if err != nil {
				log.Println(r.URL.Path, "error refreshing token: ", err)
				// attempt to do a full login if refresh fails
				bluelink_auth, err = bluelink_go.Login(MyConfig.Username, MyConfig.Password, MyConfig.Pin, MyConfig.VIN)
				if err != nil {
					log.Println(r.URL.Path, "could not relogin: ", err)
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte("500 - StatusInternalServerError"))
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

var bluelink_auth api.Auth

func DoorLockHandler(w http.ResponseWriter, r *http.Request) {
	// create climate input object
	err := bluelink_go.DoorLock(bluelink_auth)
	if err != nil {
		fmt.Println("Error locking door: ", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - Internal Server Error"))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("door locked"))
}

func StartClimateHandler(w http.ResponseWriter, r *http.Request) {

	// get airtmp from context
	ctx := r.Context()
	params := ctx.Value(ctxBlueLinkParam{}).(Params)
	// convert air temp string to int
	airtemp, err := strconv.Atoi(*params.AirTemperature)

	log.Println("starting climate with airtemp: ", airtemp)

	err = bluelink_go.StartClimate(bluelink_auth, airtemp)
	if err != nil {
		fmt.Println("Error starting climate: ", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - Internal Server Error"))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("climate started for " + *params.AirTemperature + " degrees"))

}

func StopClimateHandler(w http.ResponseWriter, r *http.Request) {
	// create climate input object
	err := bluelink_go.StopClimate(bluelink_auth)
	if err != nil {
		fmt.Println("Error stopping climate: ", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - Internal Server Error"))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("climate stopped"))
}

func GetOdometerHandler(w http.ResponseWriter, r *http.Request) {
	odometer, err := bluelink_go.GetOdometer(bluelink_auth)
	if err != nil {
		log.Println("Error GetOwnerInfo: ", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - Internal Server Error"))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(odometer))
}

func GetBatteryHandler(w http.ResponseWriter, r *http.Request) {
	var err error
	vehicleStatus, err := bluelink_go.GetVehicleStatus(bluelink_auth)
	if err != nil {
		log.Println("Error GetVehicleStatus: ", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - Internal Server Error"))
		return
	}

	w.WriteHeader(http.StatusOK)
	// convert battery status to string
	battery_string := strconv.Itoa(vehicleStatus.VehicleStatus.EvStatus.BatteryStatus)
	// convert vehicleStatus.dateTime to minutes ago
	duration := time.Since(vehicleStatus.VehicleStatus.DateTime)
	minutes := int(duration.Minutes())
	w.Write([]byte(battery_string + " percent last updated " + strconv.Itoa(minutes) + " minutes ago"))
}

/*
func GetLocationHandler(w http.ResponseWriter, r *http.Request) {
	var err error
	var force_refresh bool
	//check last read time if it's older than 5 minutes than force data refresh
	if time.Now().After(vehicle_status.ResponseString.VehicleStatus.DateTime.Add(5 * time.Minute)) {
		force_refresh = true
	} else {
		force_refresh = false
	}
	vehicle_status, err = bluelink_go.GetVehicleStatus(bluelink_auth, my_car, force_refresh)

	if err != nil {
		log.Println("Error GetVehicleStatus: ", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - Internal Server Error"))
		return
	}

	// convert battery status to string
	lon := vehicle_status.ResponseString.VehicleStatus.VehicleLocation.Coord.Lon
	lat := vehicle_status.ResponseString.VehicleStatus.VehicleLocation.Coord.Lat
	w.WriteHeader(http.StatusOK)
	// format long and lat to string up to 13 digit precision
	lon_string := strconv.FormatFloat(lon, 'f', 13, 64)
	lat_string := strconv.FormatFloat(lat, 'f', 13, 64)
	w.Write([]byte(lat_string + "," + lon_string))
}
*/

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

	// get auth object
	bluelink_auth, err = bluelink_go.Login(MyConfig.Username, MyConfig.Password, MyConfig.Pin, MyConfig.VIN)
	if err != nil {
		log.Fatal("Error logging in: ", err)
	}

	limiter = rate.NewLimiter(rate.Limit(MyConfig.RateLimit), MyConfig.RateBurst)
	mux := http.NewServeMux()

	// handle LockDoor
	mux.HandleFunc("/api/lock_door", DoorLockHandler)

	// handle Restart start_climate
	mux.HandleFunc("/api/start_climate", StartClimateHandler)
	// handle Suspend stop_climate
	mux.HandleFunc("/api/stop_climate", StopClimateHandler)
	// get odometer
	mux.HandleFunc("/api/get_odometer", GetOdometerHandler)
	// get battery
	mux.HandleFunc("/api/get_battery", GetBatteryHandler)
	// apply middle ware for rate limiting, authentication, and bluelink token refresh
	handler := Limit(Auth(RefreshBlueLink(mux)))
	return MyConfig, handler, err
}

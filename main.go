package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jasonmoo/geo"
)

func main() {
	//wundergroundAPIKey := flag.String("wunderground.api.key", "fea099f5e733c6ab", "wunderground.com API key")
	flag.Parse()

	mw := multiWeatherProvider{
		openWeatherMap{},
		// weatherUnderground{apiKey: *wundergroundAPIKey},
		forecastIo{},
	}

	http.HandleFunc("/weather/", func(w http.ResponseWriter, r *http.Request) {
		begin := time.Now()
		city := strings.SplitN(r.URL.Path, "/", 3)[2]

		temp, err := mw.temperature(city)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"city": city,
			"temp": temp,
			"took": time.Since(begin).String(),
		})
	})

	http.ListenAndServe(":8080", nil)
}

type weatherProvider interface {
	temperature(city string) (float64, error) // in Kelvin, naturally
}

type multiWeatherProvider []weatherProvider

func (w multiWeatherProvider) temperature(city string) (float64, error) {
	// Make a channel for temperatures, and a channel for errors.
	// Each provider will push a value into only one.
	temps := make(chan float64, len(w))
	errs := make(chan error, len(w))

	// For each provider, spawn a goroutine with an anonymous function.
	// That function will invoke the temperature method, and forward the response.
	for _, provider := range w {
		go func(p weatherProvider) {
			k, err := p.temperature(city)
			if err != nil {
				errs <- err
				return
			}
			temps <- k
		}(provider)
	}

	sum := 0.0

	// Collect a temperature or an error from each provider.
	for i := 0; i < len(w); i++ {
		select {
		case temp := <-temps:
			sum += temp
		case err := <-errs:
			return 0, err
		}
	}

	// Return the average, same as before.
	return sum / float64(len(w)), nil
}

type openWeatherMap struct{}

func (w openWeatherMap) temperature(city string) (float64, error) {
	resp, err := http.Get("http://api.openweathermap.org/data/2.5/weather?APPID=8339dc7784ffd8be92fd8874e8da083f&q=" + city)
	if err != nil {
		return 0, err
	}

	defer resp.Body.Close()

	var d struct {
		Main struct {
			Kelvin float64 `json:"temp"`
		} `json:"main"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return 0, err
	}

	celsius := d.Main.Kelvin - 273.15
	log.Printf("openWeatherMap: %s: %.2fK - %.2f°C", city, d.Main.Kelvin, celsius)
	return d.Main.Kelvin, nil
}

type weatherUnderground struct {
	apiKey string
}

func (w weatherUnderground) temperature(city string) (float64, error) {
	resp, err := http.Get("http://api.wunderground.com/api/" + w.apiKey + "/conditions/q/" + city + ".json")
	if err != nil {
		return 0, err
	}

	defer resp.Body.Close()

	var d struct {
		Observation struct {
			Celsius float64 `json:"temp_c"`
		} `json:"current_observation"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return 0, err
	}

	kelvin := d.Observation.Celsius + 273.15
	log.Printf("weatherUnderground: %s: %.2fK - %.2f°C", city, kelvin, d.Observation.Celsius)
	return kelvin, nil
}

type forecastIo struct{}

func (w forecastIo) temperature(city string) (float64, error) {

	// TODO get coordinates
	coords, err := geo.Geocode(city + ", france")
	if err != nil {
		return 0, err
	}

	lat := strconv.FormatFloat(float64(coords.Lat), 'f', -1, 32)
	long := strconv.FormatFloat(float64(coords.Lng), 'f', -1, 32)
	log.Printf(city+" coordinates : %s Latitude, %s Longitude", lat, long)

	resp, err := http.Get("https://api.forecast.io/forecast/432d4bab34f6496d0cb95df3d022ce12/" + lat + "," + long + "?units=si")
	if err != nil {
		return 0, err
	}

	defer resp.Body.Close()

	var d struct {
		Observation struct {
			Celsius float64 `json:"temperature"`
		} `json:"currently"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return 0, err
	}

	kelvin := d.Observation.Celsius + 273.15
	log.Printf("forecast.io: %s: %.2fK - %.2f°C", city, kelvin, d.Observation.Celsius)
	return kelvin, nil
}

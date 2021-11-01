package main

import (
	"bytes"
	"fmt"
	htmt "html/template"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	text "text/template"

	mapper "github.com/kurankat/tasmapper"
)

var dummyData string = `-42.12344,147.43321
-41.34221,145.43442
-43.22134,146.35521
-43.22133,146.35522`

var accessLog log.Logger
var errorLog log.Logger

// The main structure to hold map-related data.
type mapData struct {
	TaxonName string
	MapType   string
	RawCoords string
	SVGmap    string
}

// svgMap contains data specific to the generated SVG map to be served.
type svgMap struct {
	mapName string
	mapType string
	svgMap  string
}

// newMapData creates and initialises a mapData structure to hold data pertaining to the map
// being drawn, after cleaning up the user input
func newMapData(r *http.Request) (data *mapData) {
	data = new(mapData)

	data.TaxonName = r.FormValue("taxon")
	data.MapType = r.FormValue("maptype")
	data.RawCoords = strings.TrimSpace(strings.ReplaceAll(r.FormValue("coordinates"), " ", ""))

	return data
}

// mapSVG creates an SVG map with the data provided
func mapSVG(data *mapData) (stringMap string) {
	rl := new(mapper.RecordList)                                             // Create a new empty RecordList object
	firstRecord := strings.TrimSpace(strings.Split(data.RawCoords, "\n")[0]) // Split first line to identify type of coords given
	mapBuffer := new(bytes.Buffer)                                           // Create a new buffer to hold the map

	// Regular expressions allow 0 to 10 decimal figures in the lat and
	// Match pattern for records that contain voucher information: lat(decimal),long(decimal),voucherinfo(integer)
	voucherPattern, _ := regexp.MatchString(`^\-?\d{2}(\.\d{0,10})?,\d{3}(\.\d{0,10})?,[01]$`, firstRecord)

	// Match pattern for records that have only lat and long: lat(decimal),long(decimal)
	noVoucherPattern, _ := regexp.MatchString(`^\-?\d{2}(\.\d{0,10})?,\d{3}(\.\d{0,10})?$`, firstRecord)

	// Make a plain record list if there is no voucher info, or a voucher list if voucher info is available
	if voucherPattern {
		rl = mapper.NewVoucherRecordList(strings.NewReader(data.RawCoords), data.TaxonName)
	} else if noVoucherPattern {
		rl = mapper.NewRecordList(strings.NewReader(data.RawCoords), data.TaxonName)
	} else {
		errorLog.Println("Coordinates contain an error in the first line and cannot be interpreted", firstRecord)
		return "I can't interpret these coordinates"
	}

	switch data.MapType { // Select map type to draw depending on user input on page
	case "grid": // for grid maps
		if voucherPattern { // draw a map with solid circles for vouchered specimens
			mapper.VoucherMap(rl, mapBuffer) // and empty circles for anecdotal records
		} else {
			mapper.GridMap(rl, mapBuffer) // and a plain grid map for lat,long data
		}
	case "plain":
		mapper.ExactMap(rl, mapBuffer)
	case "web":
		mapper.WebMap(rl, mapBuffer)
	}

	return mapBuffer.String()
}

// ### Below are the three handlers for the three separate pages that are served ###

// mapAsFile will serve the SVG map as a file rather than inline, if a map
// file is in memory
func (svm *svgMap) mapAsFile(w http.ResponseWriter, r *http.Request) {
	if svm.svgMap == "" { // If the URL for mapfile is accessed directly, return error message
		errorLog.Println("Attempt to access map from memory before a map is generated")
		fmt.Fprint(w, "There is no map in memory")
	} else { // If there is a map in memory, serve it as an SVG image with calculated filename
		fileName := fmt.Sprintf("attachment; filename=%s", svm.mapName)
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Content-Disposition", fileName)
		fmt.Fprint(w, svm.svgMap)
	}
	return
}

// parsingError checks whether the templates can be parsed correctly and stops
// execution of subroutine if they can't.
// Work on better handling this so the user is returned to "/"
func parsingError(err error, w http.ResponseWriter, filename string) {
	if err != nil {
		fmt.Fprintf(w, "<h1>Map could not be rendered</h1><p>Error parsing template file: %s</p>", filename)
		errorLog.Printf("Error parsing template file: %s", filename)
	}
}

// mapDisplay handles displaying a page with results, including the generated map
// as inline SVG. A reference to an svgMap object serves for data sharing
func (svm *svgMap) mapDisplay(w http.ResponseWriter, r *http.Request) {
	r.ParseForm() // Parse all the form information

	if r.Method == "POST" { // If the request is a form submission
		// Create a new mapData object and populate its variables from user input
		data := newMapData(r)
		pageTitle := "Preview map for " + data.TaxonName
		svm.mapType = data.MapType
		svm.mapName = strings.ReplaceAll(strings.ToLower(data.TaxonName), " ", "-") +
			"." + svm.mapType + ".svg"
		svm.svgMap = mapSVG(data)
		data.SVGmap = svm.svgMap

		// Parse the various page templates and execute them in succession to build the page html.
		head, err := htmt.ParseFiles("assets/head.html")
		if err == nil {
			head.Execute(w, pageTitle)
		} else {
			parsingError(err, w, "head.html")
		}

		header, err := htmt.ParseFiles("assets/header.html")
		if err == nil {
			header.Execute(w, data)
		} else {
			parsingError(err, w, "header.html")
		}
		body, err := text.ParseFiles("assets/svg.html")
		if err == nil {
			body.Execute(w, data)
		} else {
			parsingError(err, w, "svg.html")
		}
		footer, err := htmt.ParseFiles("assets/footer.html")
		if err == nil {
			footer.Execute(w, data)
		} else {
			parsingError(err, w, "footer.html")
		}
	} else {
		http.Redirect(w, r, "/", 301)
	}
}

// dataEntry handles requests to the main page and presents a form for data entry.
// Form submission directs user to "/map", where the SVG map will be rendered
func dataEntry(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	// Normal requests to this page should be GET. If so, process the dataEntry template and serve it.
	if r.Method == "GET" {
		pageText := map[string]string{
			"title":           "Data entry form",
			"placeHolderText": "Please enter array of coordinates, in comma-delimited format, in decimal degrees",
		}

		head, err := htmt.ParseFiles("assets/head.html")
		parsingError(err, w, "head.html")
		head.Execute(w, pageText["title"])

		header, err := htmt.ParseFiles("assets/header.html")
		parsingError(err, w, "header.html")
		header.Execute(w, nil)

		body, err := htmt.ParseFiles("assets/dataEntry.html")
		parsingError(err, w, "dataEntry.html")
		body.Execute(w, pageText)

		footer, err := htmt.ParseFiles("assets/footer.html")
		parsingError(err, w, "footer.html")
		footer.Execute(w, nil)
	}
}

// style serves style.css stylesheet
func style(w http.ResponseWriter, r *http.Request) {
	stylesheet, err := htmt.ParseFiles("assets/style.css")
	w.Header().Set("Content-Type", "text/css")
	parsingError(err, w, "style.css")
	stylesheet.Execute(w, nil)
}

// Only serves three pages: "/map" for the generated SVG map, "/mapfile" for the
// generated SVG file and "/" for everything else
func main() {
	accessLog.SetOutput(os.Stdout)
	errorLog.SetOutput(os.Stderr)
	svgm := new(svgMap)
	http.HandleFunc("/", dataEntry)
	http.HandleFunc("/map", svgm.mapDisplay)
	http.HandleFunc("/mapfile", svgm.mapAsFile)
	http.HandleFunc("/style.css", style)

	err := http.ListenAndServe(":9090", nil) // setting listening port
	if err != nil {
		errorLog.Fatal("ListenAndServe: ", err)
	}
}

package main

// export AWS_REGION=us-west-2

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/polly"
	"github.com/error454/lms"
)

type AudioZone struct {
	AlsaName string
	MAC      string
}

type AudioAlertType struct {
	LmsIP       string
	WebPort     string
	AudioIntro  string
	AudioWakeup string
	Zones       map[string]AudioZone
}

var config AudioAlertType

func check(e error) {
	if e != nil {
		panic(e)
	}
}

// Read a JSON config file from disk.
func readConfigFromDisk(path string) error {
	content, err := ioutil.ReadFile(path)
	if err != nil {
		fmt.Println("No config file defined, please see sample config file.")
	}

	dec := json.NewDecoder(bytes.NewReader(content))
	err = dec.Decode(&config)
	return err
}

// Determine if an http passed zone parameter is enabled, not nil and equal to 1
func zoneParameterEnabled(s string) bool {
	return s != "" && s == "1"
}

func hash(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

// Update the audio map with our new file
func updateAudioMap(text string, filename string) {
	f, err := os.OpenFile(path.Join("audio", "map"), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	check(err)
	defer f.Close()
	f.WriteString(filename + " " + text + "\n")
}

func downloadTTSFile(text string, filename string) {

	// Create a new polly session
	sess := session.Must(session.NewSession())
	svc := polly.New(sess)

	// Set parameters for our output file
	params := &polly.SynthesizeSpeechInput{
		OutputFormat: aws.String("mp3"),
		Text:         aws.String(text),
		VoiceId:      aws.String("Salli"),
		SampleRate:   aws.String("22050"),
	}

	// Initiate the request
	resp, err := svc.SynthesizeSpeech(params)
	check(err)

	// Read the audio data
	body, err := ioutil.ReadAll(resp.AudioStream)
	check(err)

	// Write audio to destination file
	ioutil.WriteFile(path.Join("audio", filename), body, 0644)

	// Update our audio map
	updateAudioMap(text, filename)
}

// Given a string of text, return the path to the mp3 file
func getTTSFilePath(text string) string {
	fmt.Println("Getting audio for string: ", text)
	fmt.Println("Hash: ", hash(text))

	// TODO: The TTS system ignores punctuation, so we can improve
	// this so that we are more likely to get a cache hit.
	audioFileName := strconv.FormatUint(hash(text), 10) + ".mp3"
	ex, err := os.Getwd()
	check(err)

	filePath := path.Join(ex, "audio", audioFileName)

	// Does the file exist?
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		fmt.Println("File does not exist, running TTS")
		downloadTTSFile(text, audioFileName)
	} else {
		fmt.Println("The file exists")
	}

	return filePath
}

// The server handles GET requests. This is the entry point for requests.
func server(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, "Full URL: "+r.URL.RawQuery)

	// Read all of the URL parameters
	text := r.URL.Query().Get("text")
	if text == "" {
		return
	}

	// Build up an array of zones that we need to alert, these will be used
	// to easily loop through our hash table to blast out alerts.
	var alertZones []string
	for zone := range config.Zones {
		// Gets 1 or 0 based on GET parameters passed in
		zoneStatus := r.URL.Query().Get(zone)

		if zoneParameterEnabled(zoneStatus) {
			alertZones = append(alertZones, zone)
		}
	}

	// Get the path of the audio file to play
	audioPath := getTTSFilePath(text)
	io.WriteString(w, "\nGot TTS file path: "+audioPath)

	io.WriteString(w, "\nActive Zones:")
	fmt.Println("Active Zones:")
	for zone := range alertZones {
		io.WriteString(w, alertZones[zone]+"\n")
		fmt.Println(alertZones[zone])
	}

	// Play the audio
	orchestrateAudioInZones(audioPath, alertZones)
}

func playAudioInZone(audio string, zone string, preSleep int, postSleep int) {
	fmt.Println("Playing audio for zone: " + zone + " Alsa Name: " + config.Zones[zone].AlsaName)
	cmd := "mpg123"
	args := []string{"-a", config.Zones[zone].AlsaName, audio}

	fmt.Println("Executing audio command")
	time.Sleep(time.Second * time.Duration(preSleep))
	exec.Command(cmd, args...).Run()
	time.Sleep(time.Second * time.Duration(postSleep))
}

type zoneState struct {
	zone      string
	isPlaying bool
	volume    int
}

// Send back a full list of valid zones along with zones that need audio
// to be dimmed
func getValidAndDimZones(zones []string) ([]string, []zoneState) {
	var validzones []string
	dimZones := make([]zoneState, 0)

	for zone := range zones {
		state := lms.GetStreamState(config.Zones[zones[zone]].MAC)
		if state != lms.INVALID {
			validzones = append(validzones, zones[zone])

			if state == lms.PLAY {
				originalVolume := lms.GetVolume(config.Zones[validzones[zone]].MAC)
				dimZones = append(dimZones, zoneState{zone: validzones[zone], isPlaying: true, volume: originalVolume})
			}
		}
	}
	return validzones, dimZones
}

// TODO: Fade volume, no need to pause AND set volume lower. Pick one.
func fadeAudioZone(dimZone zoneState, fadeUp bool, wg *sync.WaitGroup) {
	mac := config.Zones[dimZone.zone].MAC
	volume := dimZone.volume

	wg.Add(1)
	go func() {
		lms.PauseStream(mac, !fadeUp)

		if fadeUp {
			lms.SetVolume(mac, int(float64(volume)))
		} else {
			lms.SetVolume(mac, int(float64(volume)*0.5))
		}
		wg.Done()
	}()
}

func playAudioInZones(audio string, zones []string, preSleep int, postSleep int) {
	var wg sync.WaitGroup

	if _, err := os.Stat(audio); os.IsNotExist(err) {
	} else {
		for zone := range zones {
			wg.Add(1)
			zonename := zones[zone]
			go func() {
				playAudioInZone(audio, zonename, preSleep, postSleep)
				wg.Done()
			}()
		}
		wg.Wait()
	}
}

func orchestrateAudioInZones(audio string, zones []string) {
	var wg sync.WaitGroup

	// remove any zones that are in an invalid state
	validzones, dimZones := getValidAndDimZones(zones)

	// Dim the audio in all zones if needed, track which ones were playing
	for zone := range dimZones {
		// Dim/pause all zones simultaneously
		fadeAudioZone(dimZones[zone], false, &wg)
	}

	// Wait for all goroutines to finish dimming/pausing their audio
	wg.Wait()

	// Play Wakeup audio to give amplifier time to warm up. Play audio intro and
	// then the actual audio.
	playAudioInZones(config.AudioWakeup, validzones, 0, 1)
	playAudioInZones(config.AudioIntro, validzones, 0, 0)
	playAudioInZones(audio, validzones, 1, 1)

	// If the zone was previously playing, unpause it and restore the volume level
	for zone := range dimZones {
		fadeAudioZone(dimZones[zone], true, &wg)
	}

	wg.Wait()
}

func main() {
	e := readConfigFromDisk("config")
	check(e)
	fmt.Println("Intro audio is: " + config.AudioIntro)
	fmt.Println("Wakeup audio is: " + config.AudioWakeup)
	lms.Connect(config.LmsIP)
	fmt.Println("Starting http server on port " + config.WebPort)
	http.HandleFunc("/", server)
	http.ListenAndServe(":"+config.WebPort, nil)
}

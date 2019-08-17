package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"

	"cloud.google.com/go/compute/metadata"
	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
	"github.com/sotah-inc/steamwheedle-cartel/pkg/act"
	"github.com/sotah-inc/steamwheedle-cartel/pkg/logging"
	"github.com/sotah-inc/steamwheedle-cartel/pkg/logging/stackdriver"
	"github.com/sotah-inc/steamwheedle-cartel/pkg/sotah"
	"github.com/sotah-inc/steamwheedle-cartel/pkg/sotah/codes"
	"github.com/sotah-inc/steamwheedle-cartel/pkg/state/run"
)

var port int
var serviceName string
var projectId string
var state run.WorkloadState

func init() {
	var err error

	// resolving project-id
	projectId, err = metadata.Get("project/project-id")
	if err != nil {
		log.Fatalf("Failed to get project-id: %s", err.Error())

		return
	}

	// resolving service name
	serviceName = os.Getenv("K_SERVICE")

	// establishing log verbosity
	logVerbosity, err := logrus.ParseLevel("info")
	if err != nil {
		logging.WithField("error", err.Error()).Fatal("Could not parse log level")

		return
	}
	logging.SetLevel(logVerbosity)

	// adding stackdriver hook
	logging.WithField("project-id", projectId).Info("Creating stackdriver hook")
	stackdriverHook, err := stackdriver.NewHook(projectId, serviceName)
	if err != nil {
		logging.WithFields(logrus.Fields{
			"error":     err.Error(),
			"projectID": projectId,
		}).Fatal("Could not create new stackdriver logrus hook")

		return
	}
	logging.AddHook(stackdriverHook)

	// done preliminary setup
	logging.WithField("service", serviceName).Info("Initializing service")

	// parsing http port
	port, err = strconv.Atoi(os.Getenv("PORT"))
	if err != nil {
		log.Fatalf("Failed to get port: %s", err.Error())

		return
	}
	logging.WithField("port", port).Info("Initializing with port")

	// producing gateway state
	logging.WithFields(logrus.Fields{
		"project":      projectId,
		"service-name": serviceName,
		"port":         port,
	}).Info("Producing workload state")

	state, err = run.NewWorkloadState(run.WorkloadStateConfig{ProjectId: projectId})
	if err != nil {
		log.Fatalf("Failed to generate workload state: %s", err.Error())

		return
	}

	// fin
	logging.Info("Finished init")
}

func main() {
	r := mux.NewRouter()
	r.HandleFunc("/compute-pricelist-histories", func(w http.ResponseWriter, r *http.Request) {
		logging.Info("Received request")

		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			act.WriteErroneousErrorResponse(w, "Could not read request body", err)

			logging.WithFields(logrus.Fields{
				"error": err.Error(),
			}).Error("Could not read request body")

			return
		}

		tuple, err := sotah.NewRegionRealmTimestampTuple(string(body))
		if err != nil {
			act.WriteErroneousErrorResponse(w, "Could not parse request body", err)

			logging.WithFields(logrus.Fields{
				"error": err.Error(),
			}).Error("Could not parse request body")

			return
		}

		msg := state.ComputePricelistHistories(tuple)
		switch msg.Code {
		case codes.Ok:
			w.WriteHeader(http.StatusCreated)

			if _, err := fmt.Fprint(w, msg.Data); err != nil {
				logging.WithField("error", err.Error()).Error("Failed to return response")

				return
			}
		default:
			act.WriteErroneousMessageResponse(w, "State run code was invalid", msg)

			logging.WithFields(logrus.Fields{
				"code":  msg.Code,
				"error": msg.Err,
				"data":  msg.Data,
			}).Error("State run code was invalid")
		}

		logging.Info("Sent response")
	}).Methods("POST")
	http.Handle("/", r)

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}

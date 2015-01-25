package tasky

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/mux"
)

const (
	Enabled  = "Enabled"
	Disabled = "Disabled"
)

type Action uint64

const (
	Cancel Action = iota
	Pause
	Resume
	Restart
)

type Worker interface {
	// Worker name
	Name() string

	// Description of the worker and it's usage
	Usage() string

	// Execute the task
	Perform([]byte, chan []byte, chan error, chan bool)

	// Worker status
	Status() string

	// Action to be taken on ongoing task
	Signal(Action) bool

	// Maximum number of simultaneous tasks allowed
	MaxNumTasks() uint64
}

type TaskyError struct {
	Error string
}

var (
	wMut    sync.RWMutex
	workers map[string]Worker

	tMut  sync.RWMutex
	tasks map[string]*taskyTask

	apiBase string
)

func init() {
	workers = make(map[string]Worker)
	tasks = make(map[string]*taskyTask)
	apiBase = "/tasky/v1"
}

func NewWorker(w Worker) (Worker, error) {
	tw := &taskyWorker{}
	tw.w = w

	name := w.Name()

	wMut.Lock()
	workers[name] = tw
	wMut.Unlock()

	return tw, nil
}

func uuid() string {
	b := make([]byte, 16)
	_, err := io.ReadFull(rand.Reader, b)
	if err != nil {
		log.Fatal(err)
	}
	b[6] = (b[6] & 0x0F) | 0x40
	b[8] = (b[8] &^ 0x40) | 0x80
	return fmt.Sprintf("%x%x%x%x%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

type taskid struct {
	TaskId string
}

type tstat struct {
	TaskId string
	Status string
}

func handlerGetTaskStatus(rw http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	log.Println("id: ", id)

	tMut.RLock()
	t, ok := tasks[id]
	tMut.RUnlock()

	if !ok {
		e := TaskyError{"Could not found a task with given id"}
		estr, _ := json.Marshal(e)
		log.Println("estr: ", estr)
		fmt.Fprintf(rw, "%s\n", estr)
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	s := t.status()

	ts := tstat{id, s}
	log.Println("ts: ", ts)
	jsonStr, err := json.Marshal(ts)
	if err != nil {
		e := TaskyError{err.Error()}
		estr, _ := json.Marshal(e)
		log.Println("estr: ", estr)
		fmt.Fprintf(rw, "%s\n", estr)
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(rw, "%s\n", string(jsonStr))
}

func newTask(w Worker, job []byte) taskid {
	id := uuid()

	t := &taskyTask{}
	t.new(w)

	tMut.Lock()
	tasks[id] = t
	tMut.Unlock()

	go t.run(job)

	return taskid{id}
}

type ts struct {
	Tasks []taskid
}

func listTasks() ts {
	t := ts{}

	tMut.RLock()
	for k, _ := range tasks {
		if len(t.Tasks) <= 0 {
			t.Tasks = make([]taskid, 1)
			t.Tasks[0] = taskid{k}
		} else {
			t.Tasks = append(t.Tasks, taskid{k})
		}
	}
	tMut.RUnlock()

	return t
}

func handlerListTasks(rw http.ResponseWriter, r *http.Request) {
	t := listTasks()
	log.Println("tasks: ", t)
	jsonStr, err := json.Marshal(t)
	if err != nil {
		e := TaskyError{err.Error()}
		estr, _ := json.Marshal(e)
		log.Println("estr: ", estr)
		fmt.Fprintf(rw, "%s\n", estr)
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(rw, "%s\n", jsonStr)
}

func handlerNewTask(rw http.ResponseWriter, r *http.Request) {
	job, err := ioutil.ReadAll(r.Body)
	if err != nil {
		e := TaskyError{err.Error()}
		estr, _ := json.Marshal(e)
		log.Println("estr: ", estr)
		fmt.Fprintf(rw, "%s\n", estr)
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	log.Println("job: ", job)

	name := mux.Vars(r)["name"]
	log.Println("name: ", name)

	wMut.RLock()
	w, ok := workers[name]
	wMut.RUnlock()

	if !ok {
		e := TaskyError{"Could not found worker with given name"}
		estr, _ := json.Marshal(e)
		log.Println("estr: ", estr)
		fmt.Fprintf(rw, "%s\n", estr)
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	t := newTask(w, job)
	jsonStr, err := json.Marshal(t)
	if err != nil {
		e := TaskyError{err.Error()}
		estr, _ := json.Marshal(e)
		log.Println("estr: ", estr)
		fmt.Fprintf(rw, "%s\n", estr)
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(rw, "%s\n", string(jsonStr))
}

func handlerListWorkers(rw http.ResponseWriter, r *http.Request) {
	jsonStr, _ := listWorkers()

	fmt.Fprintf(rw, "%s\n", jsonStr)
}

func RegisterTaskyHandlers(r *mux.Router) {
	tr := r.PathPrefix(apiBase).Subrouter()

	ws := tr.Path("/workers").Subrouter()
	ws.Methods("GET").HandlerFunc(handlerListWorkers)

	w := tr.PathPrefix("/workers/{name}").Subrouter()
	w.Methods("POST").HandlerFunc(handlerNewTask)

	ts := tr.Path("/task").Subrouter()
	ts.Methods("GET").HandlerFunc(handlerListTasks)

	t := tr.PathPrefix("/task/{id}").Subrouter()
	t.Methods("GET").Path("/status").HandlerFunc(handlerGetTaskStatus)
}

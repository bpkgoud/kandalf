package workers

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"kandalf/config"
	"kandalf/logger"
)

// The value of pause time to prevent CPU overload
var infiniteCycleTimeout time.Duration = 2 * time.Second

type Worker struct {
	die       chan bool
	reload    chan bool
	mutex     *sync.Mutex
	wg        *sync.WaitGroup
	isWorking bool
}

type internalWorker interface {
	run(*sync.WaitGroup, chan bool)
}

// Returns new instance of worker
func NewWorker() *Worker {
	return &Worker{
		die:    make(chan bool, 1),
		reload: make(chan bool),
		mutex:  &sync.Mutex{},
		wg:     &sync.WaitGroup{},
	}
}

// Main working cycle
func (w *Worker) Run(wgMain *sync.WaitGroup, dieMain chan bool) {
	defer wgMain.Done()

	w.isWorking = true

	go w.doRun()

	for {
		select {
		case <-dieMain:
			w.isWorking = false
			return
		default:
		}

		// Prevent CPU overload
		time.Sleep(infiniteCycleTimeout)
	}
}

// Reloads the worker
func (w *Worker) Reload() {
	w.reload <- true
}

// Launches the internal workers and executes them infinitely
func (w *Worker) doRun() {
	var (
		die     chan bool
		err     error
		wg      *sync.WaitGroup
		workers []internalWorker
	)

	for w.isWorking {
		wg = &sync.WaitGroup{}
		die = make(chan bool)
		workers, err = w.getWorkers()

		if err != nil {
			logger.Instance().
				WithError(err).
				Error("Unable to get list of the workers")

			return
		}

		go func() {
			for {
				select {
				case <-w.reload:
					logger.Instance().Info("Caught reload signal. Will stop all workers")

					close(die)

					return
				default:
				}

				// Prevent CPU overload
				time.Sleep(infiniteCycleTimeout)
			}
		}()

		wg.Add(len(workers))
		for _, w := range workers {
			go w.run(wg, die)
		}
		wg.Wait()
	}
}

// Returns list of the internal workers
func (w *Worker) getWorkers() (workers []internalWorker, err error) {
	var (
		c *internalConsumer
		q *internalQueue
	)

	q, err = newInternalQueue()
	if err != nil {
		return nil, fmt.Errorf("An error occured while instantiating queue: %v", err)
	}

	for _, url := range config.Instance().UList("rabbitmq.urls") {
		c, err = newInternalConsumer(url.(string), q)
		if err != nil {
			logger.Instance().
				WithError(err).
				WithField("url", url).
				Warning("Unable to create consumer")
		} else {
			workers = append(workers, c)

			logger.Instance().
				WithError(err).
				WithField("url", url).
				Debug("Created a new consumer")
		}
	}

	if len(workers) == 0 {
		return nil, errors.New("Haven't found any consumer or all of them failed to connect")
	}

	workers = append(workers, q)

	return workers, nil
}
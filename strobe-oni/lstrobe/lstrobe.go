package main

import (
	"context"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/DiegoBoy/godirwalk"
	"github.com/Microsoft/CorrelationVector-Go/correlationvector"
	"github.com/apex/log"
	"github.com/apex/log/handlers/text"
	"github.com/jessevdk/go-flags"
	"github.com/pkg/errors"
)

type options struct {
	Verbose []bool `short:"v" long:"verbose" description:"Show verbose debug information (multiple levels)"`

	// positional arguments
	Args struct {
		Paths []string `positional-arg-name:"path" description:"Paths used as crawling starting point"`
	} `positional-args:"yes" required:"yes"`
}

func main() {
	// parse command line args
	opts := parseArgs()
	targetPaths := getTargetPaths(opts)

	// initialize the logger
	initLogger(opts, text.New(os.Stdout))
	logger, ctx := getStageContext("main", context.TODO())
	defer logger.WithField("paths", targetPaths).Trace("lstrobe").Stop(nil)

	// init file enumerator
	lstrobe := NewLStrobe()
	lstrobe.AddDisallowedRegex("/proc/.?") // disallow /proc and all children
	lstrobe.Start(ctx)
	
	// start crawlers from all paths supplied (default = current directory)
	for _, path := range targetPaths {
		_, iterCtx := getIterationContext(ctx)
		lstrobe.AddScan(path, iterCtx)
	}

	go func() {
		for range time.Tick(time.Second * 5) {
			logger.Info("ping")
		}
	}()

	// wait for scanners to finish
	lstrobe.Wait()
	lstrobe.Stop(ctx)
}

func getTargetPaths(opts *options) []string {
	if len(opts.Args.Paths) > 0 {
		return opts.Args.Paths
	} else if pwd, err := os.Getwd(); err == nil {
		return []string{pwd, "/"}
	} else {
		return []string{"."}
	}
}

func initLogger(opts *options, handler log.Handler) {
	// log level increases with verbosity
	switch n := len(opts.Verbose); {
	case n >= 1:
		log.SetLevel(log.DebugLevel)
	default:
		log.SetLevel(log.InfoLevel)
	}

	// use a different handler for different output format
	log.SetHandler(handler)
}

func getIterationContext(ctx context.Context) (log.Interface, context.Context) {
	// if cv exists increase it
	logger := log.FromContext(ctx)
	if cv, ok := ctx.Value("cv").(*correlationvector.CorrelationVector); ok {
		newCv := cv.Increment()
		return logger.WithField("cv", newCv), ctx
	} else {
		logger.WithError(errors.New("No valid CV in context")).Error("incrementCV")
		return logger, ctx
	} 
}

func getStageContext(stage string, ctx context.Context) (log.Interface, context.Context) {
	if ctx == nil {
		return log.FromContext(nil), nil
	} 
	
	// add cv and stage to context
	cv, cvCtx := extendOrCreateCV(ctx)
	newCtx := context.WithValue(cvCtx, "stage", stage)
	
	// add cv and stage to context
	logger := log.FromContext(newCtx).WithFields(log.Fields{
		"stage": stage,
		"cv": cv.Value(),
	})
	return logger, log.NewContext(newCtx, logger)
}

func extendOrCreateCV(ctx context.Context) (*correlationvector.CorrelationVector, context.Context) {
	// if cv exists extend it
	var newCv *correlationvector.CorrelationVector
	if cv, ok := ctx.Value("cv").(*correlationvector.CorrelationVector); ok {
		var err error
		if newCv, err = correlationvector.Extend(cv.Value()); err != nil {
			log.FromContext(ctx).WithError(err).Error("extendCV")
		}
	} else { // create new one
		rand.Seed(time.Now().UnixNano())
		newCv = correlationvector.NewCorrelationVector()
	}

	// add cv to context
	return newCv, context.WithValue(ctx, "cv", newCv)
}

func parseArgs() *options {
	var opts options
	if _, err := flags.Parse(&opts); err != nil {
		/*
			passing help flag in args prints help and also throws ErrHelp
			if error type is ErrHelp, omit second print and exit cleanly
			everything else log and exit with error
		*/
		logger := log.WithError(err)
		switch flagsErrPtr := err.(type) {
		case *flags.Error:
			flagsErrType := (*flagsErrPtr).Type
			if flagsErrType == flags.ErrHelp {
				os.Exit(0)
			}
			logger.WithField("type", flagsErrType)
		default:
			logger.WithField("type", flagsErrPtr)
		}
		logger.Fatal("args")
	}
	return &opts
}

// ################################################### lib: LStrobe (public) ###################################################

// LStrobe wraps functionality to enumerate the file system.
type LStrobe struct {
	disallowedPaths map[string]struct{}         // predetermined set of files and dirs always skipped (literals)
	disallowedRegex map[*regexp.Regexp]struct{} // predetermined set of files and dirs always skipped (regex)
	repo            lstrobeRepo                 // repository for enumerated files and dirs
	waitGroup       *sync.WaitGroup             // sync barrier used to wait for enumeration to finish
	workCh          chan *lstrobeWork           // channel to distribute enumeration work
	scratchBuffer   []byte                      // use as buffer for gowalkdir to reduce garbage collection
	started         bool                        // only start work distributor once
}

func NewLStrobe() *LStrobe {
	lstrobe := LStrobe{
		disallowedPaths: make(map[string]struct{}),
		disallowedRegex: make(map[*regexp.Regexp]struct{}),
		repo:            newInMemRepo(),
		scratchBuffer:   make([]byte, 1024*8),
		started:		 false,
		waitGroup:       &sync.WaitGroup{},
		workCh:          make(chan *lstrobeWork, 4),
	}
	return &lstrobe
}

func (ls *LStrobe) AddDisallowedPath(path string) {
	ls.disallowedPaths[path] = struct{}{}
}

func (ls *LStrobe) AddDisallowedRegex(regexStr string) {
	regex := regexp.MustCompile(regexStr)
	ls.disallowedRegex[regex] = struct{}{}
}

func (ls *LStrobe) AddScan(rootPath string, ctx context.Context) {
	logger, stageCtx := getStageContext("crawl", ctx)
	ls.waitGroup.Add(1)
	ls.workCh <- &lstrobeWork{rootPath, stageCtx}
	logger.WithField("path", rootPath).Debug("queued")
}

func (ls *LStrobe) ScanFile(filePath string, ctx context.Context) {
	getStageContext("scanFile", ctx)
	log.FromContext(ctx).WithField("path", filePath).Debug("scanFile")
}

func (ls *LStrobe) Start(ctx context.Context) bool {
	logger := log.FromContext(ctx)

	// coordinators should be started just once
	if ls.started {
		logger.Debug("already started")
		return false
	}
	
	// start coordinator
	go ls.coordinator()

	// update started state
	ls.started = true
	logger.Debug("start")
	return true
}

func (ls *LStrobe) Stop(ctx context.Context) bool {
	logger := log.FromContext(ctx)

	// coordinators should be stopped just once
	if !ls.started {
		logger.Debug("already stopped")
		return false
	}

	// shut down work channel stops coordinators
	close(ls.workCh)

	// update started state
	ls.started = false
	logger.Debug("stop")
	return true
}

func (ls *LStrobe) Wait() {
	ls.waitGroup.Wait()
}

// ################################################### lib: LStrobe (private) ###################################################

func (ls *LStrobe) coordinator() {
	for work := range ls.workCh {		
		ls.worker(work)
	}
	if ls.started {
		log.WithError(errors.New("finished prematurely")).Error("coordinator")
	}
}

func (ls *LStrobe) worker(work *lstrobeWork) {
	var err error
	logger := log.FromContext(work.ctx).WithField("path", work.path)
	defer logger.TraceDebug("coordinator").StopDebug(&err)
	defer ls.waitGroup.Done()
	
	absPath, err := filepath.Abs(work.path)
	if err == nil {
		err = godirwalk.Walk(absPath, &godirwalk.Options{
			Callback:             ls.getWalkCallbackWithContext(work.ctx),
			PostChildrenCallback: ls.getPostChildrenCallbackWithContext(work.ctx),
			ErrorCallback:        ls.getErrorCallbackWithContext(work.ctx),
			Unsorted:             true,  // set true for faster yet non-deterministic enumeration (see godirwalk's godoc)
			FollowSymbolicLinks:  false, // following symlinks can loop infinitely, coordinator + walk handle symlinks instead
			ScratchBuffer:        ls.scratchBuffer,
			AllowNonDirectory:    true, // walk feeds symlinks' resolved paths to coordinator - resolved path can be file or dir
		})
	}

	// ignore:
	// - skipped
	// - not exist
	// - no perms (top link cannot be read)
	if errors.Is(err, godirwalk.SkipThis) || 
	   errors.Is(err, os.ErrNotExist) ||
	   errors.Is(err, os.ErrPermission) {
		err = nil
	}
}

func (ls *LStrobe) getWalkCallbackWithContext(ctx context.Context) func(osPathname string, de *godirwalk.Dirent) (err error) {
	return func(osPathname string, de *godirwalk.Dirent) (err error) {
		return ls.walkCallback(osPathname, de, ctx)
	}
}

func (ls *LStrobe) getPostChildrenCallbackWithContext(ctx context.Context) func(osPathname string, de *godirwalk.Dirent) (err error) {
	return func(osPathname string, de *godirwalk.Dirent) (err error) {
		return ls.postChildrenCallback(osPathname, de, ctx)
	}
}

func (ls *LStrobe) getErrorCallbackWithContext(ctx context.Context) func(osPathname string, err error) (godirwalk.ErrorAction) {
	return func(osPathname string, err error) (godirwalk.ErrorAction) {
		return ls.errorCallback(osPathname, err, ctx)
	}
}

func (ls *LStrobe) isDisallowed(path string) bool {
	if _, ok := ls.disallowedPaths[path]; ok {
		return true
	} else {
		for regex, _ := range ls.disallowedRegex {
			if regex.MatchString(path) {
				return true
			}
		}
		return false
	}
}

// walkCallback is the visitor function for each node
func (ls *LStrobe) walkCallback(osPathname string, de *godirwalk.Dirent, ctx context.Context) (error) {
	logger, ctx := getIterationContext(ctx)
	logger = logger.WithFields(log.Fields{
		"path":    osPathname,
		"mode":    de.ModeType(),
		"name":    de.Name(),
	})

	if ls.repo.isVisited(osPathname) || ls.isDisallowed(osPathname) {
		logger.Debug("skipped")
		return godirwalk.SkipThis
	} 
	
	var err error
	switch {
	case de.IsDir():
		// do nothing here, wait until all contents have been processed (postChildrenCallback)
		logger = logger.WithField("type", "dir")
	case de.IsRegular():
		// process as regular here or send to regular queue
		logger = logger.WithField("type", "regular")
		ls.ScanFile(osPathname, ctx)
	case de.IsSymlink():
		logger = logger.WithField("type", "symlink")

		// extract symlink path
		linkedFile, err := filepath.EvalSymlinks(osPathname)
		if err != nil {
			// if evalsymlinks fails, try readlink
			linkedFile, err = os.Readlink(osPathname)
			if err != nil {
				err = nil
				break
			} else if !filepath.IsAbs(linkedFile) {
				linkedFile = filepath.Join(osPathname, linkedFile)
			}
		}
		logger = logger.WithField("linkPath", linkedFile)

		// enum linked node and all parent directories in case there's partial access
		// e.g. if link points to /a/b/c/d/e/f, there might be multiple readable subpaths: /a, /a/b/c, /a/b/c/d/e/f
		// iteration starts at direct parent directory and ends in filesystem's root, e.g. /a/b/c, /a/b, /a, /
		for i := len(linkedFile); i > 0; i = strings.LastIndex(linkedFile[:i], string(os.PathSeparator)) {
			// add to crawl queue
			parentDir := linkedFile[:i]
			go ls.AddScan(parentDir, ctx)
			logger.WithField("parentDir", parentDir).Debug("walkLink")
		}
	default:
		err = godirwalk.SkipThis
	}

	// don't set dirs as visited yet
	// we do it in postChildrenCallback once all children elements were crawled
	if !de.IsDir() {
		ls.repo.setVisited(osPathname)
	}

	logger.Debug("visited")
	return err
}

// postChildrenCallback is invoked by directory nodes after all children items in a
// directory finish executing walkCallback
func (ls *LStrobe) postChildrenCallback(osPathname string, de *godirwalk.Dirent, ctx context.Context) (error) {
	ls.repo.setVisited(osPathname)
	log.FromContext(ctx).WithFields(log.Fields{
		"path": osPathname,
		"name": de.Name(),
		"type": "postDir",
	}).Debug("visited")
	return nil
}

// errorCallback is the function invoked by errors in the runtime or the other callback functions
func (ls *LStrobe) errorCallback(osPathname string, err error, ctx context.Context) (godirwalk.ErrorAction) {
	logger := log.FromContext(ctx).WithField("path", osPathname)
	
	if errors.Is(err, os.ErrPermission) {
		// get users needed to get access
		// send to queue of crawled but unaccessible
	} else {
		logger.WithError(err).Error("errorCallback")
	}
	return godirwalk.SkipNode
}

// ################################################### lib: lstrobeRepo ###################################################

// LStrobeRepo is the data store for LStrobe
type lstrobeRepo interface {
	isVisited(path string) bool
	setVisited(path string)
}

// inMemRepo is the in-memory implementation of lstrobeRepo interface
type inMemRepo struct {
	repo         lstrobeRepo         // implment interface
	visitedPaths map[string]struct{} // files and dirs already enumerated
	writeMutex   *sync.RWMutex       // prevent concurrent writes to visitedPaths
}

func newInMemRepo() *inMemRepo {
	return &inMemRepo{
		visitedPaths: make(map[string]struct{}),
		writeMutex:   &sync.RWMutex{},
	}
}

func (repo *inMemRepo) isVisited(path string) bool {
	repo.writeMutex.RLock()
	_, ok := repo.visitedPaths[path]
	repo.writeMutex.RUnlock()
	return ok
}

func (repo *inMemRepo) setVisited(path string) {
	repo.writeMutex.Lock()
	repo.visitedPaths[path] = struct{}{}
	repo.writeMutex.Unlock()
}

// ################################################### lib: lstrobeWork ###################################################

// lstrobeWork is a wrapper for path to scan and related context
type lstrobeWork struct {
	path string
	ctx  context.Context
}

package workers

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"

	"github.com/akila/document-converter/converters"
	"github.com/akila/document-converter/models"
)

type WorkerPool struct {
	JobQueue chan models.Job
	workers  int
	handler  func(models.Job)
	wg       sync.WaitGroup
}

func NewWorkerPool(workers int, handler func(models.Job)) *WorkerPool {
	return &WorkerPool{
		JobQueue: make(chan models.Job, 100),
		workers:  workers,
		handler:  handler,
	}
}

func (p *WorkerPool) Start(ctx context.Context) {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go func(workerID int) {
			defer p.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-p.JobQueue:
					if !ok {
						return
					}
					p.handler(job)
				}
			}
		}(i)
	}
}

func (p *WorkerPool) Wait() {
	close(p.JobQueue)
	p.wg.Wait()
}

// Engines
type EngineManager struct {
	LibreOfficePool *WorkerPool
	PopplerPool     *WorkerPool
	ImageMagickPool *WorkerPool
	PandocPool      *WorkerPool
	GhostscriptPool *WorkerPool
}

func NewEngineManager(numCPU int) *EngineManager {
	mgr := &EngineManager{}

	mgr.LibreOfficePool = NewWorkerPool(numCPU, func(job models.Job) {
		log.Printf("[%s] LibreOffice worker starting: %s -> %s", job.ID, job.FromFormat, job.ToFormat)
		outputPath := filepath.Join(job.TempDir, "output."+job.ToFormat)
		err := converters.LibreOfficeConvert(job.InputPath, job.TempDir, job.ToFormat)

		if err == nil {
			// Find the actual output file (LibreOffice might rename it)
			matches, _ := filepath.Glob(filepath.Join(job.TempDir, "*."+job.ToFormat))
			log.Printf("[%s] LibreOffice matches for %s: %v", job.ID, job.ToFormat, matches)
			if len(matches) > 0 {
				outputPath = matches[0]
			} else {
				// Try case-insensitive or common variations if needed, but for now just fail with info
				err = fmt.Errorf("conversion succeeded but no output file found in %s for format %s", job.TempDir, job.ToFormat)
			}
		}

		if err != nil {
			log.Printf("[%s] LibreOffice worker failed: %v", job.ID, err)
		} else {
			log.Printf("[%s] LibreOffice worker finished: %s", job.ID, outputPath)
		}

		job.ResultChan <- models.JobResult{
			Success: err == nil,
			Error:   err,
			Path:    outputPath,
		}
	})

	mgr.PopplerPool = NewWorkerPool(numCPU, func(job models.Job) {
		var err error
		outputPath := filepath.Join(job.TempDir, "output")
		if job.ToFormat == "txt" {
			outputPath = outputPath + ".txt"
			err = converters.ExtractText(job.InputPath, outputPath)
		} else {
			// Image format
			err = converters.PDFToImage(job.InputPath, outputPath, job.ToFormat)
			// pdftoppm appends -1.jpg, so we need to find it
			matches, _ := filepath.Glob(outputPath + "*." + job.ToFormat)
			if len(matches) > 0 {
				outputPath = matches[0]
			}
		}
		job.ResultChan <- models.JobResult{
			Success: err == nil,
			Error:   err,
			Path:    outputPath,
		}
	})

	mgr.ImageMagickPool = NewWorkerPool(numCPU, func(job models.Job) {
		outputPath := filepath.Join(job.TempDir, "output.pdf")
		err := converters.ImageToPDF([]string{job.InputPath}, outputPath)
		job.ResultChan <- models.JobResult{
			Success: err == nil,
			Error:   err,
			Path:    outputPath,
		}
	})

	mgr.PandocPool = NewWorkerPool(numCPU, func(job models.Job) {
		outputPath := filepath.Join(job.TempDir, "output.pdf")
		err := converters.PandocConvert(job.InputPath, outputPath)
		job.ResultChan <- models.JobResult{
			Success: err == nil,
			Error:   err,
			Path:    outputPath,
		}
	})

	mgr.GhostscriptPool = NewWorkerPool(numCPU, func(job models.Job) {
		outputPath := filepath.Join(job.TempDir, "output.pdf")
		err := converters.CompressPDF(job.InputPath, outputPath)
		job.ResultChan <- models.JobResult{
			Success: err == nil,
			Error:   err,
			Path:    outputPath,
		}
	})

	return mgr
}

func (m *EngineManager) MergePDFsSync(inputs []string, output string) error {
	return converters.MergePDFs(inputs, output)
}

func (m *EngineManager) ImageToPDFSync(inputs []string, output string) error {
	return converters.ImageToPDF(inputs, output)
}

func (m *EngineManager) Start(ctx context.Context) {
	m.LibreOfficePool.Start(ctx)
	m.PopplerPool.Start(ctx)
	m.ImageMagickPool.Start(ctx)
	m.PandocPool.Start(ctx)
	m.GhostscriptPool.Start(ctx)
}

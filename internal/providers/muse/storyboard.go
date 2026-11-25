package muse

import (
	"fmt"
	"slices"
	"time"
)

// maxBoardFrames caps a storyboard: one image per scene, and the images go
// with the video's own images in one message.
const maxBoardFrames = 6

// A storyboard video is two jobs. The image job asks Muse for BoardFrames
// scene images (boardImageLines); the video job waits for it (After) and,
// once it succeeded, attaches its images as Frames before its own references
// (releaseAfter) in a new chat of its own. Each job is sent once like any
// other, so a storyboard never pays twice for either step. Tried on muse.ai
// (HaoHao, 2026-10-09): drawing the frames first kept one person, outfit,
// room and product across scenes where a direct video changed them.
// Muse's video tool has no image-to-video mode: an attached image is a
// reference it redraws the scene from, never a literal first frame (frame 0
// of test clips matched the attached image at SSIM 0.30-0.69, TestLiveConsole
// 2026-11-11), so the scene images are worded as references.

// boardImageLines ask for the storyboard frames of a video.
func boardImageLines(job MuseJob) []string {
	return []string{
		fmt.Sprintf("Storyboard: create %d separate scene images in order (image 1 is scene 1), keeping characters, outfits, settings, and product markings consistent across scenes, no video yet.", job.BoardFrames),
	}
}

// boardVideoLines tell the video job how to use its storyboard frames.
func boardVideoLines(job MuseJob) []string {
	return []string{
		fmt.Sprintf("Storyboard: image 1 to image %d are scene references in order (image k is scene k); keep each scene's people, setting, and composition while taking motion from the prompt.", len(job.Frames)),
	}
}

// releaseWaiting attaches finished storyboards to the video jobs still
// waiting for them, in case the app stopped between the two. A video whose
// storyboard was removed, or succeeded without images, can never get its
// frames, so it fails instead of waiting forever; one whose storyboard failed
// keeps waiting for that storyboard to be retried.
func (p *Provider) releaseWaiting() {
	active := p.jobs.listActive()
	var afterIDs []string
	seenAfter := map[string]bool{}
	for _, j := range active {
		if waitsForBoard(j) && j.Status == MuseJobQueued && !j.mayBePaid() && !seenAfter[j.After] {
			seenAfter[j.After] = true
			afterIDs = append(afterIDs, j.After)
		}
	}
	if len(afterIDs) == 0 {
		return
	}
	byID := p.jobs.snapshot(afterIDs)
	// reasons holds the jobs to fail and why; the other listed jobs show
	// boardFailedStep. All are changed in one journal write.
	reasons := map[string]string{}
	var ids []string
	var boards []MuseJob
	released := map[string]bool{}
	for _, j := range active {
		if !waitsForBoard(j) || j.Status != MuseJobQueued || j.mayBePaid() {
			continue
		}
		board, ok := byID[j.After]
		switch {
		case !ok:
			reasons[j.ID] = "Storyboard đã bị xoá; tạo lại video để làm storyboard mới."
		case board.Status == MuseJobSucceeded && len(board.Files) == 0:
			reasons[j.ID] = "Storyboard không có ảnh; tạo lại video để làm storyboard mới."
		case board.Status == MuseJobSucceeded && j.BoardFrames > 0 && len(board.Files) < j.BoardFrames:
			reasons[j.ID] = fmt.Sprintf("Storyboard chỉ tạo được %d/%d ảnh; tạo lại video để làm storyboard mới.", len(board.Files), j.BoardFrames)
		case board.Status == MuseJobSucceeded:
			if !released[board.ID] {
				released[board.ID] = true
				boards = append(boards, board)
			}
			continue
		case board.Status == MuseJobFailed || board.Status == MuseJobCancelled:
			if j.Step == boardFailedStep {
				continue
			}
		default:
			continue
		}
		ids = append(ids, j.ID)
	}
	p.updateJobs(ids, func(v *MuseJob) bool {
		if !waitsForBoard(*v) || v.Status != MuseJobQueued || v.mayBePaid() {
			return false
		}
		if reason, fail := reasons[v.ID]; fail {
			v.Status, v.Error, v.Step = MuseJobFailed, reason, ""
			v.FinishedAt = p.now().UTC().Format(time.RFC3339)
			return true
		}
		if v.Step == boardFailedStep {
			return false
		}
		v.Step = boardFailedStep
		return true
	})
	for _, board := range boards {
		p.releaseAfter(board)
	}
}

// boardFailedStep is shown on a video while its storyboard has failed.
const boardFailedStep = "Storyboard lỗi; thử lại storyboard để làm video"

// waitsForBoard reports whether a job still waits for its storyboard. Once
// the frames are on the job it no longer needs the storyboard job, which may
// since have been cleared from the queue.
func waitsForBoard(job MuseJob) bool {
	return job.After != "" && len(job.Frames) == 0
}

// releaseAfter gives the video jobs waiting for a storyboard that just
// succeeded its images. Jobs already sent keep the frames they were sent with.
func (p *Provider) releaseAfter(board MuseJob) {
	if board.Status != MuseJobSucceeded || len(board.Files) == 0 {
		return
	}
	waiting := func(j MuseJob) bool {
		return j.After == board.ID && j.Status == MuseJobQueued && !j.mayBePaid()
	}
	var ids []string
	for _, j := range p.jobs.listActive() {
		if waiting(j) {
			ids = append(ids, j.ID)
		}
	}
	p.updateJobs(ids, func(v *MuseJob) bool {
		if !waiting(*v) {
			return false
		}
		frames := slices.Clone(board.Files)
		if len(frames) > v.BoardFrames && v.BoardFrames > 0 {
			frames = frames[:v.BoardFrames]
		}
		v.Frames = frames
		v.Step = "Đã có storyboard"
		if v.Review {
			v.Held = true
			v.Paused = true
			v.Step = "Đã có storyboard, xem rồi bấm Chạy để làm video"
		}
		return true
	})
	p.trigger()
}

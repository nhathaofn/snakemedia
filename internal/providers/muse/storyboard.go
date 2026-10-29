package muse

import (
	"fmt"
	"slices"
	"time"
)

// maxBoardFrames caps a storyboard: one opening frame per scene, and the
// frames go with the video's own images in one message.
const maxBoardFrames = 6

// A storyboard video is two jobs. The image job asks Muse for BoardFrames
// opening frames (boardImageLines); the video job waits for it (After) and,
// once it succeeded, attaches its images as Frames before its own references
// (releaseAfter) in a new chat of its own. Each job is sent once like any
// other, so a storyboard never pays twice for either step. Tried on muse.ai
// (HaoHao, 2026-10-09): drawing the frames first kept one person, outfit,
// room and product across scenes where a direct video changed them.

// boardImageLines ask for the storyboard frames of a video.
func boardImageLines(job MuseJob) []string {
	return []string{
		fmt.Sprintf("Storyboard: make exactly %d separate images, the opening frame of each scene of the video the prompt describes, in its scene order (image 1 opens scene 1).", job.BoardFrames),
		"Photorealistic, like stills from the finished video. The same people with the same faces, hair and clothing, the same setting, light and products in every frame; a product attached as an image stays exactly as shown, with its text and colors.",
		"Make only these images, no video yet, and add no text, captions, numbers, grids or logos to them.",
	}
}

// boardVideoLines tell the video job how to use its storyboard frames.
func boardVideoLines(job MuseJob) []string {
	n := len(job.Frames)
	return []string{
		fmt.Sprintf("Storyboard frames: image 1 to image %d are the opening frames of the video's scenes, in order (image k opens scene k). Start each scene from its frame and keep the same people, faces, clothing, setting, light and products as in the frames throughout. Do not show the frames as stills or add anything drawn on them.", n),
	}
}

// v2vLines tell a job to recreate an attached video's movement.
func v2vLines(job MuseJob) []string {
	return []string{
		"Source video: the attached video (video 1) is the template. First study its camera angles and movement, shot timing, the people's actions and pacing, and its sound style.",
		fmt.Sprintf("Then make a new %s that follows the same camera movement, timing and actions with the changes the prompt asks for. Do not send the source video back or cut it; generate new footage.", kindWord[job.Kind]),
	}
}

// releaseWaiting attaches finished storyboards to the video jobs still
// waiting for them, in case the app stopped between the two. A video whose
// storyboard was removed, or succeeded without images, can never get its
// frames, so it fails instead of waiting forever; one whose storyboard failed
// keeps waiting for that storyboard to be retried.
func (p *Provider) releaseWaiting() {
	all := p.jobs.list()
	byID := make(map[string]MuseJob, len(all))
	for _, j := range all {
		byID[j.ID] = j
	}
	// reasons holds the jobs to fail and why; the other listed jobs show
	// boardFailedStep. All are changed in one journal write.
	reasons := map[string]string{}
	var ids []string
	var boards []MuseJob
	released := map[string]bool{}
	for _, j := range all {
		if !waitsForBoard(j) || j.Status != MuseJobQueued || j.mayBePaid() {
			continue
		}
		board, ok := byID[j.After]
		switch {
		case !ok:
			reasons[j.ID] = "Storyboard đã bị xoá; tạo lại video để làm storyboard mới."
		case board.Status == MuseJobSucceeded && len(board.Files) == 0:
			reasons[j.ID] = "Storyboard không có ảnh; tạo lại video để làm storyboard mới."
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
	for _, j := range p.jobs.list() {
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
			v.Step = "Đã có storyboard, xem rồi bấm Chạy để làm video"
		}
		return true
	})
	p.trigger()
}

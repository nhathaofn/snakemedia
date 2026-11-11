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
		fmt.Sprintf("Storyboard: make exactly %d separate images, one per scene of the video the prompt describes, in scene order (image 1 shows scene 1).", job.BoardFrames),
		"Match the medium, visual style and mood requested for the finished video; do not default to photorealism. Keep recurring people, faces, hair, clothing, places and products consistent wherever they appear, except for changes the prompt explicitly describes. Show only the people and objects called for in each scene. A product reference keeps its text and colors. If scene boundaries are unspecified, divide the existing content into the requested number of frames without inventing events.",
		"Make only these separate images, no video yet. Add no storyboard labels, captions, panel numbers, grids or decorative logos. Preserve required product markings and text explicitly requested in the scene.",
	}
}

// boardVideoLines tell the video job how to use its storyboard frames.
func boardVideoLines(job MuseJob) []string {
	n := len(job.Frames)
	return []string{
		fmt.Sprintf("Storyboard: image 1 to image %d show the video's scenes in order (image k is scene k). Use each as the reference for its scene's people, wardrobe, setting, light, products and composition, subject to reference roles and locks; follow the prompt's entrances, exits and changes. They are references, not frames to show.", n),
	}
}

// sampleLines tell a job to remake an attached sample video. Muse has no
// video-to-video: its agent reads the file, looks at a few stills from it and
// writes a new prompt, passing at most a still to the video tool; it does not
// listen to the sound (TestLiveConsole, 2026-11-11). So the result is a
// similar new video, and lines must come from the prompt.
func sampleLines(job MuseJob) []string {
	return []string{
		fmt.Sprintf("Sample video: video 1 is a sample to remake as a new, similar %s. Look at it and follow its shots, camera movement, timing and actions, except what the prompt changes; appearance follows the prompt and any reference roles and locks. Speech comes only from the prompt. Do not copy the sample's identities, brands, captions or music unless asked.", kindWord[job.Kind]),
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

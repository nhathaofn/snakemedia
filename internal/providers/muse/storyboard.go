package muse

import (
	"fmt"
	"slices"
	"time"

	"SnakeMedia/internal/logger"
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
	for _, j := range p.jobs.list() {
		if !waitsForBoard(j) || j.Status != MuseJobQueued || j.mayBePaid() {
			continue
		}
		board, ok := p.jobs.get(j.After)
		reason := ""
		switch {
		case !ok:
			reason = "Storyboard đã bị xoá; tạo lại video để làm storyboard mới."
		case board.Status == MuseJobSucceeded && len(board.Files) == 0:
			reason = "Storyboard không có ảnh; tạo lại video để làm storyboard mới."
		case board.Status == MuseJobSucceeded:
			p.releaseAfter(board)
			continue
		case board.Status == MuseJobFailed || board.Status == MuseJobCancelled:
			if j.Step != boardFailedStep {
				_, _ = p.updateJob(j.ID, func(v *MuseJob) { v.Step = boardFailedStep })
			}
			continue
		default:
			continue
		}
		_, err := p.updateJob(j.ID, func(v *MuseJob) {
			v.Status, v.Error, v.Step = MuseJobFailed, reason, ""
			v.FinishedAt = p.now().UTC().Format(time.RFC3339)
		})
		if err != nil {
			logger.Get().Warn("cannot fail orphaned storyboard video", "job", j.ID, "error", err)
		}
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
	for _, j := range p.jobs.list() {
		if j.After != board.ID || j.Status != MuseJobQueued || j.mayBePaid() {
			continue
		}
		frames := slices.Clone(board.Files)
		if len(frames) > j.BoardFrames && j.BoardFrames > 0 {
			frames = frames[:j.BoardFrames]
		}
		_, err := p.updateJob(j.ID, func(v *MuseJob) {
			v.Frames = frames
			v.Step = "Đã có storyboard"
			if v.Review {
				v.Held = true
				v.Step = "Đã có storyboard, xem rồi bấm Chạy để làm video"
			}
		})
		if err != nil {
			logger.Get().Warn("cannot attach storyboard", "job", j.ID, "error", err)
		}
	}
	p.trigger()
}

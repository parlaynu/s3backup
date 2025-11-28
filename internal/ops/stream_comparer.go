package ops

import (
	"context"
	"os"
	"strings"
)

// This operator is key to the system. It accepts two input streams of EntryInfo objects.
// The first stream is the live filesystem and the second the manifest or reference
// filesystem. Based on a diff algorithm between the items and their meta-data, the
// system determines the state of the file on disk compared to the manifest. This can
// then be used by other operators to decide, for example, if the file needs to be
// backed up or not.
func NewStreamComparer(ctx context.Context, inFsys <-chan *EntryInfo, inMani <-chan *EntryInfo) <-chan *EntryInfo {

	out := make(chan *EntryInfo, 10)
	sc := streamComparer{
		ctx:    ctx,
		inFsys: inFsys,
		inMani: inMani,
		out:    out,
	}
	go sc.run()

	return out
}

type streamComparer struct {
	ctx    context.Context
	inFsys <-chan *EntryInfo
	inMani <-chan *EntryInfo
	out    chan<- *EntryInfo
}

func compare_paths(path1, path2 string) int {
	path1s := strings.Split(path1, string(os.PathSeparator))
	path2s := strings.Split(path2, string(os.PathSeparator))

	len1 := len(path1s)
	len2 := len(path2s)

	minlen := min(len1, len2)

	for i := 0; i < minlen; i++ {
		if path1s[i] < path2s[i] {
			return -1
		}
		if path1s[i] > path2s[i] {
			return 1
		}
	}

	return 0
}

func (sc *streamComparer) run() {
	defer close(sc.out)

	var hFsys *EntryInfo
	var hMani *EntryInfo

	for {
		select {
		case <-sc.ctx.Done():
			return
		default:
		}

		// read from the fsys channel
		if hFsys == nil {
			if info, ok := <-sc.inFsys; ok {
				if info.Action != NoAction {
					sc.out <- info
					continue
				}
				hFsys = info
			}
		}

		// read from the manifest channel
		if hMani == nil {
			if info, ok := <-sc.inMani; ok {
				if info.Action != NoAction {
					sc.out <- info
					continue
				}
				hMani = info
			}
		}

		// both filesystem and manifest scans are finished: all done
		if hFsys == nil && hMani == nil {
			break
		}

		// manifest scan completed but not the filesystem: a new item
		if hFsys != nil && hMani == nil {
			hFsys.Status = StatusNew
			sc.out <- hFsys
			hFsys = nil
			continue
		}

		// filesystem scan completed but not the manifest: a removed item
		if hFsys == nil && hMani != nil {
			hMani.Status = StatusNotFound
			sc.out <- hMani
			hMani = nil
			continue
		}

		// compare the paths by path segment, not as strings
		val := compare_paths(hFsys.RelPath, hMani.RelPath)

		// if hFsys is behind hMani: a new item
		if val < 0 {
			hFsys.Status = StatusNew
			sc.out <- hFsys
			hFsys = nil
			continue
		}

		// if hFsys is ahead of hMani: a removed item
		if val > 0 {
			hMani.Status = StatusNotFound
			sc.out <- hMani
			hMani = nil
			continue
		}

		// if the relpaths are the same, check the attributes
		if val == 0 {
			// initialise the status and hash
			hFsys.Status = StatusOk
			hFsys.Hash = hMani.Hash

			// if size or modtime are different, flag as changed (or potentially changed)
			if hFsys.RawSize != hMani.RawSize || hFsys.ModTime != hMani.ModTime {
				hFsys.Status = StatusModified
			}
			sc.out <- hFsys
		}

		// consume both heads
		hFsys = nil
		hMani = nil
	}
}

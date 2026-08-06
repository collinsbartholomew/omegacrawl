package repair

import (
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/user/clone/internal/httpclient"
	"github.com/user/clone/internal/storage"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

// downloadMissing fetches the given URLs and saves them under the standard
// path layout, populating rep with the outcome.
func downloadMissing(fs *storage.Filesystem, urls map[string]bool, ua string, workers int, rep *Report) error {
	list := make([]string, 0, len(urls))
	for u := range urls {
		list = append(list, u)
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)

	for _, u := range list {
		wg.Add(1)
		go func(rawURL string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			err := downloadOne(fs, rawURL, ua)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				rep.AssetsFailed++
				rep.FailureURLs = append(rep.FailureURLs, rawURL)
			} else {
				rep.AssetsDownloaded++
			}
		}(u)
	}
	wg.Wait()
	return nil
}

func downloadOne(fs *storage.Filesystem, rawURL, ua string) error {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "*/*")
	resp, err := httpclient.GlobalClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if _, copyErr := io.Copy(io.Discard, resp.Body); copyErr != nil {
			util.LogDebug("failed to discard repair response body", zap.Error(copyErr), zap.String("url", rawURL))
		}
		return errors.New("status " + resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil || len(body) == 0 {
		return errors.New("empty body")
	}
	_, err = fs.SaveFile(rawURL, body, resp.Header.Get("Content-Type"))
	return err
}

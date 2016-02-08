// Copyright 2016 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.
//
// Author: Spencer Kimball (spencer.kimball@gmail.com)

package main

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/cockroachdb/cockroach/util/log"
)

// TODO(spencer): combine this code with the code in stargazers
//   for a single utility.

const (
	githubAPI = "https://api.github.com/"
)

const (
	// tinyPR threshold of additions and deletions.
	tinyPR = 20
	// smallPR threshold of additions and deletions.
	smallPR = 100
	// mediumPR threshold of additions and deletions.
	mediumPR = 500
	// largePR threshold of additions and deletions.
	largePR = 1000
)

var ignoreRegexp = []*regexp.Regexp{
	regexp.MustCompile(`.*\.pb\.(go|cc|h)`),
	regexp.MustCompile(`.*\.css`),
}

func skipFile(f string) bool {
	for _, ire := range ignoreRegexp {
		if ire.MatchString(f) {
			return true
		}
	}
	return false
}

type User struct {
	Login            string `json:"login"`
	ID               int    `json:"id"`
	AvatarURL        string `json:"avatar_url"`
	GravatarID       string `json:"gravatar_id"`
	URL              string `json:"url"`
	HtmlURL          string `json:"html_url"`
	FollowersURL     string `json:"followers_url"`
	FollowingURL     string `json:"following_url"`
	StarredURL       string `json:"starred_url"`
	SubscriptionsURL string `json:"subscriptions_url"`
	Type             string `json:"type"`
	SiteAdmin        bool   `json:"site_admin"`
	Name             string `json:"name"`
	Company          string `json:"company"`
	Blog             string `json:"blog"`
	Location         string `json:"location"`
	Email            string `json:"email"`
	Hireable         bool   `json:"hireable"`
	Bio              string `json:"bio"`
	PublicRepos      int    `json:"public_repos"`
	PublicGists      int    `json:"public_gists"`
	Followers        int    `json:"followers"`
	Following        int    `json:"following"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`

	//GistsURL          string `json:"gists_url"`
	//OrganizationsURL  string `json:"organizations_url"`
	//ReposURL          string `json:"repos_url"`
	//EventsURL         string `json:"events_url"`
	//ReceivedEventsURL string `json:"received_events_url"`
}

type File struct {
	SHA         string `json:"sha"`
	Filename    string `json:"filename"`
	Status      string `json:"status"`
	Additions   int    `json:"additions"`
	Deletions   int    `json:"deletions"`
	Changes     int    `json:"changes"`
	BlobURL     string `json:"blob_url"`
	RawURL      string `json:"raw_url"`
	ContentsURL string `json:"contents_url"`
	Patch       string `json:"patch"`
}

// Subdirectory holds name of a subdirectory and the count of changes
// to files it contains.
type Subdirectory struct {
	Name  string
	Files []*File
}

// TotalChanges returns the total of additions and deletions made to
// files within the subdirectory.
func (sd *Subdirectory) TotalChanges() int {
	total := 0
	for _, f := range sd.Files {
		total += f.Changes
	}
	return total
}

type Subdirectories []*Subdirectory

func (slice Subdirectories) Len() int {
	return len(slice)
}

func (slice Subdirectories) Less(i, j int) bool {
	return slice[i].TotalChanges() > slice[j].TotalChanges()
}

func (slice Subdirectories) Swap(i, j int) {
	slice[i], slice[j] = slice[j], slice[i]
}

type PullRequest struct {
	URL                string `json:"url"`
	ID                 int    `json:"id"`
	HtmlURL            string `json:"html_url"`
	DiffURL            string `json:"diff_url"`
	PatchURL           string `json:"patch_url"`
	IssueURL           string `json:"issue_url"`
	Number             int    `json:"number"`
	State              string `json:"state"`
	Locked             bool   `json:"locked"`
	Title              string `json:"title"`
	User               User   `json:"user"`
	Body               string `json:"body"`
	CreatedAt          string `json:"created_at"`
	Updatedat          string `json:"updated_at"`
	ClosedAt           string `json:"closed_at"`
	MergedAt           string `json:"merged_at"`
	MergeCommitSHA     string `json:"merge_commit_sha"`
	Assignee           User   `json:"assignee"`
	Milestone          string `json:"milestone"`
	CommitsURL         string `json:"commits_url"`
	Review_commentsURL string `json:"review_comments_url"`
	Review_commentURL  string `json:"review_comment_url"`
	CommentsURL        string `json:"comments_url"`
	StatusesURL        string `json:"statuses_url"`
	Merged             bool   `json:"merged"`
	Mergeable          bool   `json:"mergeable"`
	MergeableState     string `json:"mergeable_state"`
	Merged_by          User   `json:"merged_by"`
	Comments           int    `json:"comments"`
	ReviewComments     int    `json:"review_comments"`
	Commits            int    `json:"commits"`
	Additions          int    `json:"additions"`
	Deletions          int    `json:"deletions"`
	ChangedFiles       int    `json:"changed_files"`

	Files []*File `json:"-"`
}

// TotalChanges returns total of additions and deletions.
func (pr *PullRequest) TotalChanges() int {
	total := 0
	for _, f := range pr.Files {
		total += f.Changes
	}
	return total
}

// Subdirectories returns a sorted slice of subdirectories which include
// changed files, sorted by number of changes. Only the subdirectories
// which comprise <=80% of the total changes are returned.
func (pr *PullRequest) Subdirectories() []*Subdirectory {
	subdirs := map[string]*Subdirectory{}
	sds := []*Subdirectory{}
	for _, f := range pr.Files {
		dir := path.Dir(f.Filename)
		if len(dir) == 0 {
			dir = "/"
		}
		if _, ok := subdirs[dir]; !ok {
			sd := &Subdirectory{Name: dir}
			sds = append(sds, sd)
			subdirs[dir] = sd
		}
		subdirs[dir].Files = append(subdirs[dir].Files, f)
	}
	sort.Sort(Subdirectories(sds))
	total := pr.TotalChanges()
	count := 0
	for i, sd := range sds {
		count += sd.TotalChanges()
		if float64(count)/float64(total) > 0.80 {
			// Truncate the sds array to ignore uninteresting subdirectories.
			sds = sds[:i+1]
			break
		}
	}
	return sds
}

// Class returns one of "tiny", "small", "medium" or "large" depending
// on the total number of changes in the pull request.
func (pr *PullRequest) Class() string {
	if tc := pr.TotalChanges(); tc < tinyPR {
		return "&#9679;"
	} else if tc < smallPR {
		return "&#9679;&#9679;"
	} else if tc < mediumPR {
		return "&#9679;&#9679;&#9679;"
	} else if tc < largePR {
		return "&#9679;&#9679;&#9679;&#9679;"
	}
	return "&#9679;&#9679;&#9679;&#9679;&#9679;"
}

// CreatedAtStr returns created at timestap in human-readable format
// according to server-local time.
func (pr *PullRequest) CreatedAtStr() string {
	t, err := time.Parse(time.RFC3339, pr.CreatedAt)
	if err != nil {
		return pr.CreatedAt
	}
	return t.Local().Format("Mon Jan _2 15:04:05")
}

// ClosedAtStr returns closed at timestap in human-readable format
// according to server-local time.
func (pr *PullRequest) ClosedAtStr() string {
	t, err := time.Parse(time.RFC3339, pr.ClosedAt)
	if err != nil {
		return pr.ClosedAt
	}
	return t.Local().Format("Mon Jan _2 15:04:05")
}

// Queries pull requests for the repository. Returns a slice each for
// open and closed pull requests.
func Query(c *Context) ([]*PullRequest, []*PullRequest, error) {
	open, closed, err := QueryPullRequests(c)
	if err != nil {
		return nil, nil, err
	}
	if err := QueryDetailedPullRequests(c, open); err != nil {
		return nil, nil, err
	}
	if err := QueryDetailedPullRequests(c, closed); err != nil {
		return nil, nil, err
	}
	return open, closed, nil
}

// QueryPullRequests queries all pull requests from the repo or a
// day's worth, whichever is greater.
func QueryPullRequests(c *Context) ([]*PullRequest, []*PullRequest, error) {
	log.Infof("querying pull requests from %s opened or closed after %s", c.Repo, c.FetchSince.Format(time.RFC3339))
	url := fmt.Sprintf("%srepos/%s/pulls?state=all&sort=updated&direction=desc", githubAPI, c.Repo)
	open, closed := []*PullRequest{}, []*PullRequest{}
	total := 0
	var err error
	var done bool
	fmt.Printf("*** 0 open 0 closed, 0 total pull requests")
	for len(url) > 0 && !done {
		fetched := []*PullRequest{}
		url, err = fetchURL(c, url, &fetched)
		if err != nil {
			return nil, nil, err
		}
		total += len(fetched)
		for _, pr := range fetched {
			var date string
			switch pr.State {
			case "open":
				date = pr.CreatedAt
			case "closed":
				date = pr.ClosedAt
			default:
				continue
			}
			t, err := time.Parse(time.RFC3339, date)
			if err != nil {
				return nil, nil, err
			}
			if pr.State == "open" {
				if c.FetchSince.Before(t) {
					open = append(open, pr)
				}
			} else {
				if t.Before(c.FetchSince) {
					done = true
					break
				}
				closed = append(closed, pr)
			}
		}
		fmt.Printf("\r*** %s open %s closed %s total pull requests", format(len(open)), format(len(closed)), format(total))
	}
	fmt.Printf("\n")
	return open, closed, nil
}

// QueryDetailedPullRequests queries detailed info on each pull request
// in the provided slice.
func QueryDetailedPullRequests(c *Context, prs []*PullRequest) error {
	log.Infof("querying detailed info for each of %s pull requests...", format(len(prs)))
	fmt.Printf("*** detailed info for 0 pull requests")
	for i, pr := range prs {
		// Fetch detailed pull request info.
		if _, err := fetchURL(c, pr.URL, pr); err != nil {
			return err
		}
		// Fetch files changed by pull request.
		if _, err := fetchURL(c, pr.URL+"/files", &pr.Files); err != nil {
			return err
		}
		// Remove files we're supposed to ignore.
		newFiles := []*File{}
		for _, f := range pr.Files {
			if !skipFile(f.Filename) {
				newFiles = append(newFiles, f)
			}
		}
		pr.Files = newFiles
		fmt.Printf("\r*** detailed info for %s pull requests", format(i+1))
	}
	fmt.Printf("\n")
	return nil
}

func format(n int) string {
	in := strconv.FormatInt(int64(n), 10)
	out := make([]byte, len(in)+(len(in)-2+int(in[0]/'0'))/3)
	if in[0] == '-' {
		in, out[0] = in[1:], '-'
	}

	for i, j, k := len(in)-1, len(out)-1, 0; ; i, j = i-1, j-1 {
		out[j] = in[i]
		if i == 0 {
			return string(out)
		}
		if k++; k == 3 {
			j, k = j-1, 0
			out[j] = ','
		}
	}
}

package git

import (
	"errors"
	"regexp"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

var versionRegex = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

type Git struct {
	repo *git.Repository
}

func New() (*Git, error) {
	repo, err := git.PlainOpenWithOptions("", &git.PlainOpenOptions{DetectDotGit: true})
	return &Git{repo: repo}, err
}

// Revision returns the current revision (HEAD) of the repository in the format used by go pseudo versions.
func (g *Git) Revision() (string, time.Time, error) {
	commitRef, err := g.repo.Head()
	if err != nil {
		return "", time.Time{}, err
	}
	commit, err := g.repo.CommitObject(commitRef.Hash())
	if err != nil {
		return "", time.Time{}, err
	}
	return commitRef.Hash().String()[:8], commit.Author.When, nil
}

// FirstParentWithVersionTag returns the first parent of the HEAD commit (or HEAD itself) that has a version tag.
func (g *Git) FirstParentWithVersionTag() (revision string, versionTag string, err error) {
	commitRef, err := g.repo.Head()
	if err != nil {
		return "", "", err
	}
	commit, err := g.repo.CommitObject(commitRef.Hash())
	if err != nil {
		return "", "", err
	}
	commitToHash, err := g.tagsByRevisionHash()
	if err != nil {
		return "", "", err
	}

	iter := object.NewCommitIterCTime(commit, nil, nil)
	if err := iter.ForEach(
		func(c *object.Commit) error {
			tags, ok := commitToHash[c.Hash.String()]
			if !ok {
				return nil
			}
			version := g.findVersionTag(tags)
			if version == nil {
				return nil
			}
			versionTag = *version
			revision = c.Hash.String()
			return storer.ErrStop
		},
	); err != nil {
		return "", "", err
	}
	if revision == "" || versionTag == "" {
		return "", "", errors.New("no version tag found")
	}
	return revision, versionTag, nil
}

// tagsByRevisionHash returns a map from revision hash to a list of associated tags.
func (g *Git) tagsByRevisionHash() (map[string][]string, error) {
	tags := make(map[string][]string)
	refs, err := g.repo.Tags()
	if err != nil {
		return nil, err
	}
	if err := refs.ForEach(
		func(ref *plumbing.Reference) error {
			tag, err := g.repo.TagObject(ref.Hash())
			switch err {
			case nil:
				// Tag object present
			case plumbing.ErrObjectNotFound:
				// Not a tag object
				return nil
			default:
				// Some other error
				return err
			}
			commit, err := tag.Commit()
			if err != nil {
				return err
			}
			commitHash := commit.Hash.String()
			tags[commitHash] = append(tags[commitHash], tag.Name)
			return nil
		},
	); err != nil {
		return nil, err
	}
	return tags, nil
}

// findVersionTag tries to find a tag for a semantic version (e.g.: v1.0.0).
func (g *Git) findVersionTag(tags []string) *string {
	for _, tag := range tags {
		if versionRegex.MatchString(tag) {
			return &tag
		}
	}
	return nil
}

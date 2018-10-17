package models

import (
	"path"
	"strings"

	ap "github.com/mariusor/littr.go/app/activitypub"

	"github.com/buger/jsonparser"
	"github.com/juju/errors"
	log "github.com/sirupsen/logrus"

	as "github.com/mariusor/activitypub.go/activitystreams"
)

type Converter interface {
	FromActivityPubItem(ob as.Item) error
}

func (h *Hash) FromActivityPubItem(it as.Item) error {
	*h = getHashFromAP(it.GetLink())
	return nil
}

func (a *Account) FromActivityPubItem(it as.Item) error {
	if it == nil {
		return errors.New("nil item received")
	}
	if it.IsLink() {
		a.Hash.FromActivityPubItem(it.GetLink())
		return nil
	}
	switch it.GetType() {
	case as.CreateType:
		fallthrough
	case as.UpdateType:
		if act, ok := it.(*ap.Activity); ok {
			return a.FromActivityPubItem(act.Actor)
		}
	case as.PersonType:
		if p, ok := it.(*ap.Person); ok {
			a.Score = p.Score
			a.Metadata = &AccountMetadata{
				Key: &SSHKey{
					ID:     "",
					Public: []byte(p.PublicKey.PublicKeyPem),
				},
			}
			name := jsonUnescape(as.NaturalLanguageValue(p.Name).First())
			a.Hash = getHashFromAP(p)
			a.Handle = name
			a.Email = ""
			a.Flags = FlagsNone
		}
	default:
		return errors.New("invalid object type")
	}

	return nil
}

func (i *Item) FromActivityPubItem(it as.Item) error {
	if it == nil {
		return errors.New("nil item received")
	}
	if it.IsLink() {
		i.Hash.FromActivityPubItem(it.GetLink())
		return nil
	}
	switch it.GetType() {
	case as.CreateType:
		fallthrough
	case as.UpdateType:
		fallthrough
	case as.ActivityType:
		if act, ok := it.(*ap.Activity); ok {
			return i.FromActivityPubItem(act.Object)
		}
		if act, ok := it.(ap.Activity); ok {
			return i.FromActivityPubItem(act.Object)
		}
	case as.ArticleType:
		fallthrough
	case as.NoteType:
		fallthrough
	case as.DocumentType:
		fallthrough
	case as.PageType:
		if a, ok := it.(ap.Article); ok {
			i.Score = a.Score
			i.Hash = getHashFromAP(a)
			title := jsonUnescape(as.NaturalLanguageValue(a.Name).First())
			content := jsonUnescape(as.NaturalLanguageValue(a.Content).First())

			i.Hash = getHashFromAP(a)
			i.Title = title
			i.MimeType = string(a.MediaType)
			i.Data = content
			i.SubmittedAt = a.Published
			i.SubmittedBy = &Account{
				Hash: getHashFromAP(a.AttributedTo),
			}
			r := a.InReplyTo
			if p, ok := r.(as.IRI); ok {
				i.Parent = &Item{
					Hash: getHashFromAP(p),
				}
			}
			if a.Context != a.InReplyTo {
				op := a.Context
				if p, ok := op.(as.IRI); ok {
					i.OP = &Item{
						Hash: getHashFromAP(p),
					}
				}
			}
		}
	default:
		return errors.New("invalid object type")
	}

	return nil
}

func (v *Vote) FromActivityPubItem(it as.Item) error {
	if it == nil {
		return errors.New("nil item received")
	}
	if it.IsLink() {
		return errors.New("unable to load from IRI")
	}
	switch it.GetType() {
	case as.LikeType:
		fallthrough
	case as.DislikeType:
		if act, ok := it.(ap.Activity); ok {
			on := Item{}
			on.FromActivityPubItem(act.Object)
			v.Item = &on

			er := Account{}
			er.FromActivityPubItem(act.Actor)
			v.SubmittedBy = &er

			v.SubmittedAt = act.Published
			v.UpdatedAt = act.Updated
			if act.Type == as.LikeType {
				v.Weight = 1
			}
			if act.Type == as.DislikeType {
				v.Weight = -1
			}
		}
		if act, ok := it.(*ap.Activity); ok {
			on := Item{}
			on.FromActivityPubItem(act.Object)
			v.Item = &on

			er := Account{}
			er.FromActivityPubItem(act.Actor)
			v.SubmittedBy = &er

			v.SubmittedAt = act.Published
			v.UpdatedAt = act.Updated
			if act.Type == as.LikeType {
				v.Weight = 1
			}
			if act.Type == as.DislikeType {
				v.Weight = -1
			}
		}
	}

	return nil
}

func getHashFromAP(obj as.Item) Hash {
	s := strings.Split(obj.GetLink().String(), "/")
	var hash string
	if s[len(s)-1] == "object" {
		hash = s[len(s)-2]
	} else {
		hash = s[len(s)-1]
	}
	return Hash(path.Base(hash))
}

func jsonUnescape(s string) string {
	var out []byte
	var err error
	if out, err = jsonparser.Unescape([]byte(s), nil); err != nil {
		Logger.WithFields(log.Fields{}).Error(err)
		return s
	}
	return string(out)
}
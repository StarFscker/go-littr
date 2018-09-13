package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/juju/errors"

	"github.com/mariusor/littr.go/app/models"
	log "github.com/sirupsen/logrus"
)

// HandleHostMeta serves /.well-known/host-meta
func HandleHostMeta(w http.ResponseWriter, r *http.Request) {

	d := fmt.Sprintf(`{ "links": [{ "rel": "lrdd", "type": "application/xrd+json", "template":"https://%s/.well-known/webfinger?resource={uri}" }] }`, "littr.me")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(d))
}

// HandleWebFinger serves /.well-known/webfinger/ request
func HandleWebFinger(w http.ResponseWriter, r *http.Request) {
	typ, res := func(ar []string) (string, string) {
		if len(ar) != 2 {
			return "", ""
		}
		return ar[0], ar[1]
	}(strings.Split(r.URL.Query()["resource"][0], ":"))

	if typ == "" || res == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("{}"))
		return
	}

	handle := strings.Replace(res, "@littr.me", "", 1)

	val := r.Context().Value(RepositoryCtxtKey)
	AcctLoader, ok := val.(models.CanLoadAccounts)
	if ok {
		log.WithFields(log.Fields{}).Infof("loaded repository of type %T", AcctLoader)
	} else {
		err := errors.New("could not load account loader service from Context")
		log.WithFields(log.Fields{}).Error(err)
		HandleError(w, r, http.StatusInternalServerError, err)
		return
	}
	a, err := AcctLoader.LoadAccount(models.LoadAccountFilter{Handle: handle})
	if err != nil {
		err := errors.New("resource not found")
		log.WithFields(log.Fields{}).Error(err)
		HandleError(w, r, http.StatusNotFound, err)
		return
	}

	type link struct {
		Rel      string `json:"rel,omitempty"`
		Type     string `json:"type,omitempty"`
		Href     string `json:"href,omitempty"`
		Template string `json:"template,omitempty"`
	}
	type webfinger struct {
		Subject string   `json:"subject"`
		Aliases []string `json:"aliases"`
		Links   []link   `json:"links"`
	}

	wf := webfinger{
		Aliases: []string{
			fmt.Sprintf("https://%s/api/accounts/%s", "littr.me", a.Handle),
		},
		Subject: typ + ":" + res,
		Links: []link{
			link{
				Rel:  "self",
				Type: "application/activity+json",
				Href: fmt.Sprintf("https://%s/api/accounts/%s", "littr.me", a.Hash),
			},
			link{
				Rel:  "http://webfinger.net/rel/profile-page",
				Href: fmt.Sprintf("https://%s/api/accounts/%s", "littr.me", a.Hash),
			},
		},
	}

	//d = fmt.Sprintf(`{"subject": "`+typ+`:`+res+`","links": [{"rel": "self","type": "application/activity+json","href": "https://%s/api/accounts/%s", "template": null}, {"rel": "http://webfinger.net/rel/profile-page","type": null, "href": "https://%s/api/accounts/%s", "template": null},]}`, "littr.me", handle, "littr.me", handle)
	dat, _ := json.Marshal(wf)
	w.WriteHeader(http.StatusOK)
	w.Write(dat)
}

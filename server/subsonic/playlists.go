package subsonic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server/subsonic/responses"
	"github.com/navidrome/navidrome/utils"
)

func (api *Router) GetPlaylists(r *http.Request) (*responses.Subsonic, error) {
	ctx := r.Context()
	allPls, err := api.ds.Playlist(ctx).GetAll(model.QueryOptions{Sort: "name"})
	if err != nil {
		log.Error(r, err)
		return nil, err
	}
	playlists := make([]responses.Playlist, len(allPls))
	for i, p := range allPls {
		playlists[i] = *api.buildPlaylist(p)
	}
	response := newResponse()
	response.Playlists = &responses.Playlists{Playlist: playlists}
	return response, nil
}

func (api *Router) GetPlaylist(r *http.Request) (*responses.Subsonic, error) {
	ctx := r.Context()
	id, err := requiredParamString(r, "id")
	if err != nil {
		return nil, err
	}
	return api.getPlaylist(ctx, id)
}

func (api *Router) getPlaylist(ctx context.Context, id string) (*responses.Subsonic, error) {
	pls, err := api.ds.Playlist(ctx).GetWithTracks(id, true)
	if errors.Is(err, model.ErrNotFound) {
		log.Error(ctx, err.Error(), "id", id)
		return nil, newError(responses.ErrorDataNotFound, "Directory not found")
	}
	if err != nil {
		log.Error(ctx, err)
		return nil, err
	}

	response := newResponse()
	response.Playlist = api.buildPlaylistWithSongs(ctx, pls)
	return response, nil
}

func (api *Router) create(ctx context.Context, playlistId, name string, ids []string) (string, error) {
	err := api.ds.WithTx(func(tx model.DataStore) error {
		owner := getUser(ctx)
		var pls *model.Playlist
		var err error

		if playlistId != "" {
			pls, err = tx.Playlist(ctx).Get(playlistId)
			if err != nil {
				return err
			}
			if owner.ID != pls.OwnerID {
				return model.ErrNotAuthorized
			}
		} else {
			pls = &model.Playlist{Name: name}
			pls.OwnerID = owner.ID
		}
		pls.Tracks = nil
		pls.AddTracks(ids)

		err = tx.Playlist(ctx).Put(pls)
		playlistId = pls.ID
		return err
	})
	return playlistId, err
}

func (api *Router) CreatePlaylist(r *http.Request) (*responses.Subsonic, error) {
	ctx := r.Context()
	songIds := utils.ParamStrings(r, "songId")
	playlistId := utils.ParamString(r, "playlistId")
	name := utils.ParamString(r, "name")
	if playlistId == "" && name == "" {
		return nil, errors.New("required parameter name is missing")
	}
	id, err := api.create(ctx, playlistId, name, songIds)
	if err != nil {
		log.Error(r, err)
		return nil, err
	}
	return api.getPlaylist(ctx, id)
}

func (api *Router) DeletePlaylist(r *http.Request) (*responses.Subsonic, error) {
	id, err := requiredParamString(r, "id")
	if err != nil {
		return nil, err
	}
	err = api.ds.Playlist(r.Context()).Delete(id)
	if errors.Is(err, model.ErrNotAuthorized) {
		return nil, newError(responses.ErrorAuthorizationFail)
	}
	if err != nil {
		log.Error(r, err)
		return nil, err
	}
	return newResponse(), nil
}

func (api *Router) UpdatePlaylist(r *http.Request) (*responses.Subsonic, error) {
	playlistId, err := requiredParamString(r, "playlistId")
	if err != nil {
		return nil, err
	}
	songsToAdd := utils.ParamStrings(r, "songIdToAdd")
	songIndexesToRemove := utils.ParamInts(r, "songIndexToRemove")
	var plsName *string
	if s, ok := r.URL.Query()["name"]; ok {
		plsName = &s[0]
	}
	var comment *string
	if c, ok := r.URL.Query()["comment"]; ok {
		comment = &c[0]
	}
	var public *bool
	if _, ok := r.URL.Query()["public"]; ok {
		p := utils.ParamBool(r, "public", false)
		public = &p
	}

	log.Debug(r, "Updating playlist", "id", playlistId)
	if plsName != nil {
		log.Trace(r, fmt.Sprintf("-- New Name: '%s'", *plsName))
	}
	log.Trace(r, fmt.Sprintf("-- Adding: '%v'", songsToAdd))
	log.Trace(r, fmt.Sprintf("-- Removing: '%v'", songIndexesToRemove))

	err = api.playlists.Update(r.Context(), playlistId, plsName, comment, public, songsToAdd, songIndexesToRemove)
	if errors.Is(err, model.ErrNotAuthorized) {
		return nil, newError(responses.ErrorAuthorizationFail)
	}
	if err != nil {
		log.Error(r, err)
		return nil, err
	}
	return newResponse(), nil
}

func (api *Router) buildPlaylistWithSongs(ctx context.Context, p *model.Playlist) *responses.PlaylistWithSongs {
	pls := &responses.PlaylistWithSongs{
		Playlist: *api.buildPlaylist(*p),
	}
	pls.Entry = childrenFromMediaFiles(ctx, p.MediaFiles())
	return pls
}

func (api *Router) buildPlaylist(p model.Playlist) *responses.Playlist {
	pls := &responses.Playlist{}
	pls.Id = p.ID
	pls.Name = p.Name
	pls.Comment = p.Comment
	pls.SongCount = int32(p.SongCount)
	pls.Owner = p.OwnerName
	pls.Duration = int32(p.Duration)
	pls.Public = p.Public
	pls.Created = p.CreatedAt
	pls.CoverArt = p.CoverArtID().String()
	if p.IsSmartPlaylist() {
		pls.Changed = time.Now()
	} else {
		pls.Changed = p.UpdatedAt
	}
	return pls
}

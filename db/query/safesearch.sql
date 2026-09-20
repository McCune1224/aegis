-- name: ListProfileSafesearch :many
SELECT profile, engine FROM profile_safesearch ORDER BY profile, engine;

-- name: DeleteProfileSafesearch :exec
DELETE FROM profile_safesearch WHERE profile = ?;

-- name: InsertProfileSafesearch :exec
INSERT INTO profile_safesearch (profile, engine) VALUES (?, ?);

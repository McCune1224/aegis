-- name: WipeClients :execresult
DELETE FROM clients;

-- name: WipeDiscoveries :execresult
DELETE FROM discoveries;

-- name: WipeQueries :execresult
DELETE FROM queries;

-- name: WipeServices :execresult
DELETE FROM services;

-- name: WipeServiceWindows :execresult
DELETE FROM service_windows;

-- name: WipeClientRoutes :execresult
DELETE FROM routes WHERE client <> '';

-- name: WipeSettings :execresult
DELETE FROM settings;

-- name: WipeAccess :execresult
DELETE FROM access;

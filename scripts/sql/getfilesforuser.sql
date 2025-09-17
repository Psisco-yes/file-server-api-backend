SELECT id 
FROM nodes 
WHERE owner_id = (SELECT id FROM users WHERE username = :'username') 
  AND node_type = 'file' 
  AND deleted_at IS NULL; 
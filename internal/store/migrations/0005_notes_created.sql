-- What a person wrote on a day (Pocket Day asks through /pocket/day) is found
-- by when a note was made, without walking every note they have.
CREATE INDEX notes_user_created ON notes(user_id, created_at);

INSERT INTO t_admin (name, nickname, password, disabled)
VALUES (
    'admin',
    'System Admin',
    'CHANGE_ME_WITH_BCRYPT_HASH',
    FALSE
)
ON CONFLICT (name) DO NOTHING;

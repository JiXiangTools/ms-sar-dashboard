INSERT INTO t_admin (name, nickname, password, disabled)
VALUES (
    :'admin_name',
    'System Admin',
    :'admin_password_hash',
    FALSE
)
ON CONFLICT (name) DO UPDATE SET
    nickname = EXCLUDED.nickname,
    password = EXCLUDED.password,
    disabled = FALSE,
    last_update_time = NOW();

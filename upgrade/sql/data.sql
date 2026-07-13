INSERT INTO t_admin (name, nickname, password, disabled)
VALUES (
    :'admin_name',
    'System Admin',
    :'admin_password_hash',
    FALSE
)
ON CONFLICT (name) DO NOTHING;

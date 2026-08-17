-- CRM: справочник клиентов BionicPRO.
-- username соответствует username пользователя в Keycloak (reports-realm) -
-- это ключ, по которому Airflow объединяет данные CRM и телеметрии в витрину отчётности.
CREATE TABLE IF NOT EXISTS customers (
    id SERIAL PRIMARY KEY,
    username VARCHAR(255) NOT NULL UNIQUE,
    full_name VARCHAR(255) NOT NULL,
    region VARCHAR(255) NOT NULL,
    prosthetic_model VARCHAR(255),
    prosthetic_active BOOLEAN NOT NULL DEFAULT FALSE,
    contract_signed_at DATE NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- REPLICA IDENTITY FULL нужен Debezium (см. Задание 4): при DEFAULT
-- события DELETE логической репликации содержат только PK (id), а
-- витрина в ClickHouse матчит строки customers по username - без полного
-- набора старых значений строку с удалённым клиентом было бы невозможно
-- корректно пометить как удалённую.
ALTER TABLE customers REPLICA IDENTITY FULL;

INSERT INTO customers (username, full_name, region, prosthetic_model, prosthetic_active, contract_signed_at) VALUES
    ('prothetic1', 'Prothetic One', 'Moscow', 'BionicArm X1', TRUE, '2025-01-15'),
    ('prothetic2', 'Prothetic Two', 'Saint Petersburg', 'BionicArm X1', TRUE, '2025-02-20'),
    ('prothetic3', 'Prothetic Three', 'Kazan', 'BionicLeg Pro', TRUE, '2025-03-10'),
    ('john.doe', 'John Doe', 'Foreign Office', 'BionicArm X1', TRUE, '2025-01-05'),
    ('alex.johnson', 'Alex Johnson', 'Foreign Office', 'BionicLeg Pro', TRUE, '2025-04-01'),
    ('user1', 'User One', 'Moscow', NULL, FALSE, '2025-05-01'),
    ('user2', 'User Two', 'Moscow', NULL, FALSE, '2025-05-02'),
    ('jane.smith', 'Jane Smith', 'Foreign Office', NULL, FALSE, '2025-05-03')
ON CONFLICT (username) DO NOTHING;

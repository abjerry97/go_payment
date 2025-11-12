
CREATE TABLE IF NOT EXISTS customer_accounts (
    customer_id VARCHAR(50) PRIMARY KEY,
    asset_value DECIMAL(15, 2) NOT NULL DEFAULT 1000000.00,
    term_weeks INTEGER NOT NULL DEFAULT 50,
    total_paid DECIMAL(15, 2) NOT NULL DEFAULT 0.00,
    outstanding_balance DECIMAL(15, 2) NOT NULL DEFAULT 1000000.00,
    deployment_date TIMESTAMP NOT NULL,
    last_payment_date TIMESTAMP,
    payment_count INTEGER NOT NULL DEFAULT 0,
    version INTEGER NOT NULL DEFAULT 0, 
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);
 
CREATE INDEX IF NOT EXISTS idx_customer_id ON customer_accounts(customer_id);
CREATE INDEX IF NOT EXISTS idx_outstanding_balance ON customer_accounts(outstanding_balance);
 
CREATE TABLE IF NOT EXISTS processed_transactions (
    transaction_reference VARCHAR(100) PRIMARY KEY,
    customer_id VARCHAR(50) NOT NULL,
    amount DECIMAL(15, 2) NOT NULL,
    processed_at TIMESTAMP NOT NULL DEFAULT NOW(),
    FOREIGN KEY (customer_id) REFERENCES customer_accounts(customer_id)
);
 
CREATE INDEX IF NOT EXISTS idx_txn_ref ON processed_transactions(transaction_reference);
CREATE INDEX IF NOT EXISTS idx_txn_customer ON processed_transactions(customer_id);
 
CREATE TABLE IF NOT EXISTS payment_history (
    id BIGSERIAL PRIMARY KEY,
    customer_id VARCHAR(50) NOT NULL,
    transaction_reference VARCHAR(100) NOT NULL UNIQUE,
    amount DECIMAL(15, 2) NOT NULL,
    payment_status VARCHAR(20) NOT NULL,
    transaction_date TIMESTAMP NOT NULL,
    processed_at TIMESTAMP NOT NULL DEFAULT NOW(),
    balance_before DECIMAL(15, 2) NOT NULL,
    balance_after DECIMAL(15, 2) NOT NULL,
    FOREIGN KEY (customer_id) REFERENCES customer_accounts(customer_id)
);
 
CREATE INDEX IF NOT EXISTS idx_payment_customer ON payment_history(customer_id);
CREATE INDEX IF NOT EXISTS idx_payment_date ON payment_history(transaction_date);
 
CREATE OR REPLACE FUNCTION update_outstanding_balance()
RETURNS TRIGGER AS $$
BEGIN
    NEW.outstanding_balance := GREATEST(0, NEW.asset_value - NEW.total_paid);
    NEW.updated_at := NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
 
CREATE TRIGGER trigger_update_balance
    BEFORE UPDATE OF total_paid ON customer_accounts
    FOR EACH ROW
    EXECUTE FUNCTION update_outstanding_balance();
 
INSERT INTO customer_accounts (customer_id, deployment_date)
SELECT 
    'GIG' || LPAD(generate_series::TEXT, 5, '0'),
    NOW() - (random() * INTERVAL '365 days')
FROM generate_series(1, 100)
ON CONFLICT (customer_id) DO NOTHING;
 
CREATE OR REPLACE VIEW customer_statistics AS
SELECT 
    customer_id,
    asset_value,
    total_paid,
    outstanding_balance,
    payment_count,
    ROUND((total_paid / asset_value * 100)::NUMERIC, 2) as completion_percentage,
    CASE 
        WHEN outstanding_balance = 0 THEN 'COMPLETED'
        WHEN total_paid > 0 THEN 'IN_PROGRESS'
        ELSE 'NOT_STARTED'
    END as status,
    deployment_date,
    last_payment_date,
    EXTRACT(WEEK FROM AGE(NOW(), deployment_date)) as weeks_since_deployment
FROM customer_accounts;
 
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO payment_user;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO payment_user;
 
CREATE OR REPLACE FUNCTION get_payment_stats()
RETURNS TABLE (
    total_customers BIGINT,
    active_customers BIGINT,
    completed_customers BIGINT,
    total_deployed_value DECIMAL,
    total_paid_amount DECIMAL,
    total_outstanding DECIMAL,
    avg_completion_rate DECIMAL
) AS $$
BEGIN
    RETURN QUERY
    SELECT 
        COUNT(*)::BIGINT,
        COUNT(*) FILTER (WHERE total_paid > 0)::BIGINT,
        COUNT(*) FILTER (WHERE outstanding_balance = 0)::BIGINT,
        SUM(asset_value),
        SUM(total_paid),
        SUM(outstanding_balance),
        ROUND(AVG(total_paid / asset_value * 100)::NUMERIC, 2)
    FROM customer_accounts;
END;
$$ LANGUAGE plpgsql;
 
CREATE INDEX IF NOT EXISTS idx_customer_status ON customer_accounts(outstanding_balance) WHERE outstanding_balance > 0;
CREATE INDEX IF NOT EXISTS idx_recent_payments ON payment_history(processed_at DESC);
 
COMMENT ON TABLE customer_accounts IS 'Stores customer account information and balances';
COMMENT ON TABLE processed_transactions IS 'Tracks processed transactions for idempotency';
COMMENT ON TABLE payment_history IS 'Audit trail of all payments';
COMMENT ON COLUMN customer_accounts.version IS 'Used for optimistic locking to prevent race conditions';
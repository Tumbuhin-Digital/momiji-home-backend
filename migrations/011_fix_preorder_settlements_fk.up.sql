DROP TABLE IF EXISTS preorder_settlements;

CREATE TABLE preorder_settlements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_line_item_id UUID NOT NULL REFERENCES order_line_items(id) ON DELETE CASCADE,
    balance_amount NUMERIC(12,2) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    due_date DATE,
    invoiced_at TIMESTAMP WITH TIME ZONE,
    paid_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_preorder_settlements_item_id ON preorder_settlements(order_line_item_id);

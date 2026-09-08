-- 历史前三次（含无奖）使用独立概率。已有流水自然计入次数，不重置或补发。
ALTER TABLE lottery_draws ADD COLUMN intro_draw_number SMALLINT NOT NULL DEFAULT 0
    CHECK (intro_draw_number BETWEEN 0 AND 3);
ALTER TABLE lottery_draws ADD COLUMN probability_weights JSONB
    CHECK (probability_weights IS NULL OR
           (jsonb_typeof(probability_weights) = 'array' AND jsonb_array_length(probability_weights) = 7));

-- 原规则仍为万分之一，新规则为十万分之一；保持旧应用写入默认值的兼容性。
ALTER TABLE lottery_draws DROP CONSTRAINT lottery_draws_ticket_check;
ALTER TABLE lottery_draws ADD CONSTRAINT lottery_draws_ticket_check CHECK (
    ticket >= 0 AND ticket < CASE WHEN intro_draw_number > 0 THEN 100000 ELSE 10000 END
);
ALTER TABLE lottery_draws ADD CONSTRAINT lottery_intro_weights_required
    CHECK (intro_draw_number = 0 OR probability_weights IS NOT NULL);
CREATE UNIQUE INDEX lottery_draws_intro_unique ON lottery_draws(user_id, intro_draw_number)
    WHERE intro_draw_number > 0;

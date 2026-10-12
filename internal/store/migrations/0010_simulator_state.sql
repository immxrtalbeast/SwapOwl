-- Включён ли генератор заказов демо-стенда: после рестарта API он включается снова (FS-134).
-- Пишется отдельным UPDATE (SetSimulatorRunning), чтобы SaveClock колонку не затирал.
ALTER TABLE clock_state ADD COLUMN simulator_running boolean NOT NULL DEFAULT false;

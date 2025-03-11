

SET search_path = public, pg_catalog;
--
-- Data for table public.systems (LIMIT 0,13)
--

INSERT INTO public.systems (system_id, description, addr, resource, active, predefined) OVERRIDING SYSTEM VALUE
VALUES ('sys:erp', 'Управление предприятием', 'AD 57 9C A9 80 0E 4F 56 C6 6A C2 47 7E 0C 23 47', 'http://10.0.0.240/erp-adapter/hs/esb/', true, false);

INSERT INTO public.systems (system_id, description, addr, resource, active, predefined) OVERRIDING SYSTEM VALUE
VALUES ('sys:zup', 'ЗУП ТА', NULL, 'http://10.0.0.239/ta_zup_av/hs/esb/', true, false);

INSERT INTO public.systems (system_id, description, addr, resource, active, predefined) OVERRIDING SYSTEM VALUE
VALUES ('ESB', 'Шина данных', NULL, NULL, false, true);

INSERT INTO public.systems (system_id, description, addr, resource, active, predefined) OVERRIDING SYSTEM VALUE
VALUES ('DLQ', 'Недоставленные сообщения', NULL, '', false, true);
-- Data for table public.channels (LIMIT 0,16)
--
INSERT INTO public.channels (channel, system_id, channel_description, enable_route, predefined) OVERRIDING SYSTEM VALUE
VALUES ('sys:bp', 'sys:bp', 'Бухгалтерия предприятия ', true, false);

INSERT INTO public.channels (channel, system_id, channel_description, enable_route, predefined) OVERRIDING SYSTEM VALUE
VALUES ('sys:dlq', 'DLQ', 'Недоставленные сообщения', false, true);

INSERT INTO public.channels (channel, system_id, channel_description, enable_route, predefined) OVERRIDING SYSTEM VALUE
VALUES ('sys:erp', 'sys:erp', 'Управление предприятием', true, false);

INSERT INTO public.channels (channel, system_id, channel_description, enable_route, predefined) OVERRIDING SYSTEM VALUE
VALUES ('sys:esb', 'ESB', 'Необработанные сообщения', false, true);


#pragma once

class HubClient;

void serialCliPrintHelp();
void serialCliHandleLine(const char *line, HubClient &hub);

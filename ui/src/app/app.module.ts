import { BrowserModule } from '@angular/platform-browser';
import { NgModule } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HttpModule } from '@angular/http';

// Components
import { AppComponent } from './app.component';
import { BatchCountChartComponent } from './batch-count-chart/batch-count-chart.component';
import { LagGaugeComponent } from './lag-gauge/lag-gauge.component';
import { LagChartComponent } from './lag-chart/lag-chart.component';
import { LastSeenTableComponent } from './last-seen-table/last-seen-table.component';
import { ThroughtputChartComponent } from './throughput-chart/throughput-chart.component';

@NgModule({
    declarations: [
        AppComponent,
        BatchCountChartComponent,
        LagChartComponent,
        LagGaugeComponent,
        LastSeenTableComponent,
        ThroughtputChartComponent
    ],
    imports: [
        BrowserModule,
        FormsModule,
        HttpModule,
    ],
    providers: [],
    bootstrap: [AppComponent]
})
export class AppModule { }


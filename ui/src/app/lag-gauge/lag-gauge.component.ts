import { Component } from '@angular/core';
import * as _ from "lodash";

// Services
import { ChartsService } from '../charts.service';

@Component({
    selector: 'lag-gauge',
    templateUrl: './lag-gauge.component.html',
    styleUrls: ['./lag-gauge.component.scss']
})

export class LagGaugeComponent {

    gaugeObject: any;

    constructor(private chartsService: ChartsService) {
        this.gaugeObject = {
            chartType: 'Gauge',
            data: {},
            chart: null,
            domElement: 'lag-gauge',
            chartReady: false,
            columns: [['Label', 'string'], ['Lag (secs)', 'number']],
            options: {
                width: 200,
                height: 200,
                chartArea: {top: 30, left: 0, right: 100},
            }
        };

        (<any>window).google.charts.setOnLoadCallback(this.chartsService.drawGauge.bind(this, this.gaugeObject));

        let pendingData: Array<Object> = [];

        this.chartsService.getObservable().subscribe( (message: any) => {
            let data = JSON.parse(message.data);
            if (data.MetricType === 'Lag') {
                pendingData.push(data);
            }
        });

        setInterval(() => {
            _.forEach(pendingData, (data: any) => {
                if(this.gaugeObject.chartReady) {
                    this.chartsService.updateGauge(data, this.gaugeObject);
                }
            });
            pendingData.splice(0);
        }, 1000)

    }
}
